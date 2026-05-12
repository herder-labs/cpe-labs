// Command cpe-sim is the cpe-labs simulator entry point.
//
// On startup it loads a vendor profile (single file or directory),
// constructs the CWMP stack (transport, event tracker, session), and
// runs one Inform against the configured ACS, exiting 0 on a clean
// session close or 1 on any error.
//
// This is the v0 main loop: one-shot Inform per process. Periodic
// scheduling, connection-request listening, and richer handler
// registration land in subsequent stories.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/herder-labs/cpe-labs/internal/cpeconfig"
	"github.com/herder-labs/cpe-labs/internal/cpelog"
	"github.com/herder-labs/cpe-labs/internal/cperng"
	"github.com/herder-labs/cpe-labs/internal/cwmp"
	"github.com/herder-labs/cpe-labs/internal/cwmp/cr"
	"github.com/herder-labs/cpe-labs/internal/cwmp/handlers"
	"github.com/herder-labs/cpe-labs/internal/cwmp/inform"
	"github.com/herder-labs/cpe-labs/internal/cwmp/scheduler"
	"github.com/herder-labs/cpe-labs/internal/cwmp/transfer"
	"github.com/herder-labs/cpe-labs/internal/cwmp/transport"
	"github.com/herder-labs/cpe-labs/internal/generators"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	usphandlers "github.com/herder-labs/cpe-labs/internal/usp/handlers"
	"github.com/herder-labs/cpe-labs/internal/usp/identity"
	"github.com/herder-labs/cpe-labs/internal/usp/mtp"
	mqttmtp "github.com/herder-labs/cpe-labs/internal/usp/mtp/mqtt"
	"github.com/herder-labs/cpe-labs/internal/usp/notify"
	uspcodec "github.com/herder-labs/cpe-labs/internal/usp/codec"
	uspsession "github.com/herder-labs/cpe-labs/internal/usp/session"
	"github.com/herder-labs/cpe-labs/internal/version"
)

// cpeIDFmt is the per-CPE registration key format. Used as the
// scheduler registration key, the cperng split-key, and the CR
// listener path suffix when Fleet.Count > 1. When Fleet.Count == 1
// the suffix is dropped from the CR path so single-CPE deployments
// keep their existing URL shape.
const cpeIDFmt = "cpe-%d"

func main() {
	err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return
	}
	_, _ = fmt.Fprintln(os.Stderr, "cpe-sim:", err)
	os.Exit(1)
}

// cpeStack is the per-CPE state cmd/cpe-sim builds. One stack per
// simulated CPE; Fleet.Count instances of these run in parallel
// against the shared scheduler / CR listener / RNG source.
type cpeStack struct {
	id           string
	serial       string
	tree         *paramtree.Tree
	tracker      *cwmp.EventTracker
	transport    *transport.Transport
	session      *cwmp.Session
	sessionMu    *sync.Mutex
	runOpts      *cwmp.RunSessionOptions
	genRunner    *generators.Runner
	hasScheduler bool

	uspAdapter mtp.Adapter            // nil when USP disabled
	uspOpts    *uspsession.Options    // nil when USP disabled
}

func run(ctx context.Context, args []string, stdout, stderr *os.File) error {
	if hasVersionFlag(args) {
		_, _ = fmt.Fprintln(stdout, version.String())
		return nil
	}

	cfg, err := cpeconfig.Load(args, cpeconfig.EnvMap(os.Environ()))
	if err != nil {
		return err
	}

	logger, err := cpelog.New(cpelog.Options{
		Level:  cfg.LogLevel,
		Format: cfg.LogFormat,
		Writer: stderr,
	})
	if err != nil {
		return err
	}

	if cfg.ACSURL == "" {
		return fmt.Errorf("--acs-url is required")
	}

	if cfg.ProfilePath == "" {
		return fmt.Errorf("--profile is required (no built-in fallback; " +
			"the simulator is vendor-neutral and will not assume any data model — " +
			"supply a YAML profile that declares parameters, deviceIdPaths, and any optional blocks)")
	}

	// First load: read fleet config + validate deviceIdPaths. Per-CPE
	// trees are reloaded fresh below so each gets its own independent
	// tree; this load just gives us the fleet metadata.
	templateProf, err := paramtree.LoadProfile(cfg.ProfilePath)
	if err != nil {
		return fmt.Errorf("load profile: %w", err)
	}
	if templateProf.DeviceIDPaths.IsZero() {
		return fmt.Errorf("profile %q is missing the required deviceIdPaths block "+
			"(no TR-181 / TR-098 default in core code; declare which parameter paths "+
			"the inform builder reads — TR-181 uses Device.DeviceInfo.* and TR-098 uses "+
			"InternetGatewayDevice.DeviceInfo.*)", cfg.ProfilePath)
	}

	count := templateProf.Fleet.Count
	if count < 1 {
		count = 1
	}
	pattern := templateProf.Fleet.SerialPattern
	if pattern == "" {
		pattern = "{base}-{i}"
	}

	// Read the base serial from the template tree so we know what to
	// substitute when count > 1.
	baseSerialLeaf, err := templateProf.Tree.Get(templateProf.DeviceIDPaths.SerialNumber)
	if err != nil {
		return fmt.Errorf("read base serial at %q: %w", templateProf.DeviceIDPaths.SerialNumber, err)
	}
	baseSerial := baseSerialLeaf.Raw

	// Process-wide infrastructure shared across all CPEs.
	pool, err := transport.NewPool(transport.PoolOptions{
		TLSSkipVerify:  cfg.TLSSkipVerify,
		CACertFile:     cfg.CACertFile,
		DefaultTimeout: cfg.ACSTimeout,
		Logger:         logger,
	})
	if err != nil {
		return fmt.Errorf("transport pool: %w", err)
	}

	rngSource := cperng.New(cfg.Seed)
	logger.Info("rng initialized",
		"seed_supplied", cfg.Seed != 0,
		"root_seed", rngSource.RootSeed())

	sched := scheduler.NewScheduler(scheduler.Options{Logger: logger})
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := sched.Stop(shutdownCtx); shutdownErr != nil {
			logger.Warn("scheduler shutdown error", "err", shutdownErr.Error())
		}
	}()

	// CR listener (shared across the fleet; per-CPE Endpoint paths).
	var listener *cr.Listener
	if cfg.CRBindAddr != "" {
		listener, err = cr.NewListener(cr.ListenerOptions{
			BindAddr: cfg.CRBindAddr,
			Logger:   logger,
		})
		if err != nil {
			return fmt.Errorf("connection-request listener: %w", err)
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if shutdownErr := listener.Shutdown(shutdownCtx); shutdownErr != nil {
				logger.Warn("listener shutdown error", "err", shutdownErr.Error())
			}
		}()
	}

	// Build per-CPE stacks.
	stacks := make([]*cpeStack, 0, count)
	for i := 1; i <= count; i++ {
		id := fmt.Sprintf(cpeIDFmt, i)
		serial := stampSerial(pattern, baseSerial, i)
		stack, buildErr := buildCPEStack(cfg, cpeStackInputs{
			id:         id,
			serial:     serial,
			instance:   i,
			fleetCount: count,
			pool:       pool,
			rngSource:  rngSource,
			sched:      sched,
			listener:   listener,
			logger:     logger,
		})
		if buildErr != nil {
			return fmt.Errorf("build CPE %s (serial=%s): %w", id, serial, buildErr)
		}
		stacks = append(stacks, stack)
	}

	// Defer generator-runner shutdowns.
	for _, st := range stacks {
		st := st
		if st.genRunner == nil {
			continue
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if shutdownErr := st.genRunner.Stop(shutdownCtx); shutdownErr != nil {
				logger.Warn("generator runner shutdown error", "cpe_id", st.id, "err", shutdownErr.Error())
			}
		}()
	}

	hasAnyScheduler := false
	hasAnyGenerators := false
	hasAnyUSP := false
	for _, st := range stacks {
		if st.hasScheduler {
			hasAnyScheduler = true
		}
		if st.genRunner != nil {
			hasAnyGenerators = true
		}
		if st.uspOpts != nil {
			hasAnyUSP = true
		}
	}
	eventScheduleRequiresDaemon := templateProf.EventSchedule.RequiresDaemon()

	logger.Info("cpe-sim starting",
		"version", version.Version,
		"acs_url", cfg.ACSURL,
		"profile", cfg.ProfilePath,
		"cr_bind_addr", cfg.CRBindAddr,
		"fleet_count", count,
		"scheduler_enabled", hasAnyScheduler,
		"generators_enabled", hasAnyGenerators,
		"event_schedule_daemon", eventScheduleRequiresDaemon,
		"usp_enabled", hasAnyUSP,
	)

	// Start the CR listener now (after all per-CPE endpoints are
	// registered) so the listener is live before bootstrap Informs
	// fire. The published ConnectionRequestURL was written into each
	// tree at registration time below, so the bootstrap Inform carries
	// the correct URL.
	if listener != nil {
		if startErr := listener.Start(); startErr != nil {
			return fmt.Errorf("start CR listener: %w", startErr)
		}
	}

	// Bootstrap all CPEs in parallel. One slow / failing CPE shouldn't
	// gate the others. We log per-CPE outcomes; the run() return value
	// only reflects the very-first hard failure (typically transport
	// misconfig that affects everyone).
	bootstrapErr := bootstrapAll(ctx, stacks, templateProf.EventSchedule.BootDelay, logger)
	if bootstrapErr != nil {
		return bootstrapErr
	}

	// Start scheduler + generator runners after bootstrap so first
	// periodic tick is interval+jitter after BOOTSTRAP, not from
	// process start.
	if hasAnyScheduler {
		if startErr := sched.Start(ctx); startErr != nil {
			return fmt.Errorf("scheduler.Start: %w", startErr)
		}
	}
	for _, st := range stacks {
		if st.genRunner == nil {
			continue
		}
		if startErr := st.genRunner.Start(ctx); startErr != nil {
			return fmt.Errorf("generators.Start (cpe=%s): %w", st.id, startErr)
		}
	}

	// One-shot mode: exit after bootstrap if nothing is keeping us
	// alive (no listener, no scheduler, no generators, no
	// event-schedule deferral that needs to fire later, no USP).
	if listener == nil && !hasAnyScheduler && !hasAnyGenerators && !eventScheduleRequiresDaemon && !hasAnyUSP {
		return nil
	}

	signalCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("cpe-sim daemon mode",
		"listener", listener != nil,
		"scheduler", hasAnyScheduler,
		"generators", hasAnyGenerators,
		"event_schedule", eventScheduleRequiresDaemon,
		"usp", hasAnyUSP,
	)

	var uspWG sync.WaitGroup
	for _, st := range stacks {
		if st.uspOpts == nil {
			continue
		}
		uspWG.Add(1)
		go func(s *cpeStack) {
			defer uspWG.Done()
			if err := uspsession.Run(signalCtx, *s.uspOpts); err != nil {
				logger.Warn("usp session exited with error", "cpe_id", s.id, "err", err.Error())
			}
		}(st)
	}

	<-signalCtx.Done()
	logger.Info("cpe-sim shutting down")
	uspWG.Wait()
	return nil
}

// applyFleetPlaceholders walks every leaf in tree and substitutes
// fleet-level placeholders in the leaf's Raw value. Two-pass: walk
// collects (path, new-raw) pairs under the read lock, then SetSystem
// rewrites them under the write lock — Walk's fn callback runs while
// the read lock is held, so calling SetSystem from inside it would
// deadlock.
//
// Recognized placeholders (in any leaf value):
//
//	{cpe}                              — 1-based fleet instance index
//	{cpe:N}                            — instance, zero-padded to N digits
//	{cpe:hex:N} / {cpe:HEX:N}          — instance, zero-padded to N hex digits
//	{cpe:ipv4:CIDR}                    — Nth host in the IPv4 CIDR (inline form)
//	{cpe:ipv6:CIDR}                    — Nth host in the IPv6 CIDR (inline form)
//	{cpe:ipv6prefix:SUPER,SUBLEN}      — Nth /SUBLEN prefix from SUPER (DHCPv6-PD style)
//	{cpe_id}                           — assigned CPE ID (e.g. "cpe-3")
//	{<named-pool>}                     — value resolved from fleet.pools[<named-pool>]
//
// Existing path-template {i} (table-instance index) is unrelated and
// is fully resolved at profile-load time before this runs.
func applyFleetPlaceholders(tree *paramtree.Tree, instance int, cpeID string, pools map[string]paramtree.FleetPool) error {
	// Resolve named pools once per CPE so {pool_name} substitutions
	// share one allocation per pool.
	resolved := make(map[string]string, len(pools))
	for name, pool := range pools {
		v, err := paramtree.ResolvePool(pool, instance)
		if err != nil {
			return fmt.Errorf("fleet.pools[%q]: %w", name, err)
		}
		resolved[name] = v
	}

	type pending struct{ path, raw string }
	var updates []pending
	if err := tree.Walk("", -1, func(path string, v paramtree.Value) error {
		if !hasFleetPlaceholder(v.Raw, resolved) {
			return nil
		}
		next, sErr := substituteFleetPlaceholders(v.Raw, instance, cpeID, resolved)
		if sErr != nil {
			return fmt.Errorf("substitute %q: %w", path, sErr)
		}
		if next != v.Raw {
			updates = append(updates, pending{path: path, raw: next})
		}
		return nil
	}); err != nil {
		return err
	}
	for _, u := range updates {
		if err := tree.SetSystem(u.path, u.raw); err != nil {
			return fmt.Errorf("apply fleet placeholder at %q: %w", u.path, err)
		}
	}
	return nil
}

// hasFleetPlaceholder is a cheap pre-filter so the substitution
// machinery only runs on leaves that actually need it.
func hasFleetPlaceholder(s string, resolved map[string]string) bool {
	if strings.Contains(s, "{cpe}") || strings.Contains(s, "{cpe:") || strings.Contains(s, "{cpe_id}") {
		return true
	}
	for name := range resolved {
		if strings.Contains(s, "{"+name+"}") {
			return true
		}
	}
	return false
}

// substituteFleetPlaceholders rewrites every placeholder in s.
func substituteFleetPlaceholders(s string, instance int, cpeID string, resolved map[string]string) (string, error) {
	out := strings.ReplaceAll(s, "{cpe_id}", cpeID)
	out = strings.ReplaceAll(out, "{cpe}", strconv.Itoa(instance))
	for name, v := range resolved {
		out = strings.ReplaceAll(out, "{"+name+"}", v)
	}
	return expandCPEFormPlaceholders(out, instance)
}

// expandCPEFormPlaceholders walks the string finding {cpe:...} forms
// and substitutes per the format spec inside. Recognized forms:
//
//	{cpe:N}                       — zero-padded decimal
//	{cpe:hex:N} / {cpe:HEX:N}     — zero-padded hex
//	{cpe:ipv4:CIDR}               — Nth host in the IPv4 CIDR
//	{cpe:ipv6:CIDR}               — Nth host in the IPv6 CIDR
//	{cpe:ipv6prefix:SUPER,SUBLEN} — Nth /SUBLEN prefix from SUPER
//
// Anything between {cpe: and } that doesn't match a known form is
// left literal so misconfiguration is visible at the ACS rather than
// silently dropped.
func expandCPEFormPlaceholders(s string, instance int) (string, error) {
	const marker = "{cpe:"
	var b strings.Builder
	for {
		i := strings.Index(s, marker)
		if i < 0 {
			b.WriteString(s)
			break
		}
		j := strings.IndexByte(s[i:], '}')
		if j < 0 {
			b.WriteString(s)
			break
		}
		j += i
		spec := s[i+len(marker) : j]
		expanded, ok, err := evalCPEForm(spec, instance)
		if err != nil {
			return "", fmt.Errorf("{cpe:%s}: %w", spec, err)
		}
		b.WriteString(s[:i])
		if ok {
			b.WriteString(expanded)
		} else {
			b.WriteString(s[i : j+1]) // leave literal
		}
		s = s[j+1:]
	}
	return b.String(), nil
}

// evalCPEForm evaluates one {cpe:SPEC} body. Returns (value, recognized, err):
// recognized=false means the spec didn't match any known form so the
// caller leaves it literal; err is non-nil only when a recognized form
// has invalid arguments (bad CIDR, sublen out of range, etc.).
func evalCPEForm(spec string, instance int) (string, bool, error) {
	// Plain decimal width: {cpe:N}.
	if w, err := strconv.Atoi(spec); err == nil && w >= 0 {
		return fmt.Sprintf("%0*d", w, instance), true, nil
	}
	// Other forms use "kind:arg" or "kind:arg,arg2".
	colon := strings.IndexByte(spec, ':')
	if colon < 0 {
		return "", false, nil
	}
	kind, arg := spec[:colon], spec[colon+1:]
	switch kind {
	case "hex":
		w, err := strconv.Atoi(arg)
		if err != nil || w < 0 {
			return "", true, fmt.Errorf("hex width %q: invalid", arg)
		}
		return fmt.Sprintf("%0*x", w, instance), true, nil
	case "HEX":
		w, err := strconv.Atoi(arg)
		if err != nil || w < 0 {
			return "", true, fmt.Errorf("HEX width %q: invalid", arg)
		}
		return fmt.Sprintf("%0*X", w, instance), true, nil
	case "mac", "MAC":
		// NIC portion of a MAC: arg is the byte count (1..3). Produces
		// colon-separated zero-padded hex. {cpe:mac:3} for instance 1
		// → "00:00:01"; for instance 65536 → "01:00:00". Operator
		// pre-pends the vendor OUI: "00:00:C5:{cpe:mac:3}".
		w, err := strconv.Atoi(arg)
		if err != nil || w < 1 || w > 3 {
			return "", true, fmt.Errorf("mac byte-count %q: invalid (want 1..3)", arg)
		}
		maxInst := uint64(1)<<uint(w*8) - 1
		if uint64(instance) > maxInst {
			return "", true, fmt.Errorf("mac:%d byte-count: instance %d exceeds capacity %d", w, instance, maxInst)
		}
		fmtRune := "x"
		if kind == "MAC" {
			fmtRune = "X"
		}
		var bytes []string
		for i := w - 1; i >= 0; i-- {
			b := byte((instance >> uint(i*8)) & 0xff)
			bytes = append(bytes, fmt.Sprintf("%02"+fmtRune, b))
		}
		return strings.Join(bytes, ":"), true, nil
	case "ipv4":
		v, err := paramtree.ResolvePool(paramtree.FleetPool{Type: "ipv4", CIDR: arg}, instance)
		if err != nil {
			return "", true, err
		}
		return v, true, nil
	case "ipv6":
		v, err := paramtree.ResolvePool(paramtree.FleetPool{Type: "ipv6", CIDR: arg}, instance)
		if err != nil {
			return "", true, err
		}
		return v, true, nil
	case "ipv6prefix":
		// arg is "SUPER,SUBLEN".
		comma := strings.LastIndexByte(arg, ',')
		if comma < 0 {
			return "", true, fmt.Errorf("ipv6prefix arg %q: expected SUPER,SUBLEN", arg)
		}
		super := arg[:comma]
		subLen, err := strconv.Atoi(arg[comma+1:])
		if err != nil {
			return "", true, fmt.Errorf("ipv6prefix sublen %q: %w", arg[comma+1:], err)
		}
		v, err := paramtree.ResolvePool(paramtree.FleetPool{Type: "ipv6prefix", Super: super, SubLen: subLen}, instance)
		if err != nil {
			return "", true, err
		}
		return v, true, nil
	}
	return "", false, nil
}

// stampSerial applies pattern to baseSerial / instance, returning the
// per-CPE serial. Recognized placeholders:
//
//	{base}    — the SerialNumber the profile declared (template default)
//	{i}       — the 1-based instance index, no padding
//	{i:N}     — the 1-based instance index, zero-padded to N digits
//	            (e.g. {i:04} → 0001 for instance 1, 0042 for instance 42)
//
// Unknown placeholder forms are left literal so misconfiguration is
// visible at the ACS rather than silently dropped.
func stampSerial(pattern, baseSerial string, instance int) string {
	out := strings.ReplaceAll(pattern, "{base}", baseSerial)
	out = strings.ReplaceAll(out, "{i}", strconv.Itoa(instance))
	// Substitute {i:N} → zero-padded instance to N digits.
	out = padIPlaceholder(out, instance)
	return out
}

// padIPlaceholder finds {i:N} placeholders and substitutes the
// zero-padded instance index. Single-pass scan; non-matching braces
// stay literal.
func padIPlaceholder(s string, instance int) string {
	const marker = "{i:"
	var b strings.Builder
	for {
		i := strings.Index(s, marker)
		if i < 0 {
			b.WriteString(s)
			break
		}
		j := strings.IndexByte(s[i:], '}')
		if j < 0 {
			b.WriteString(s)
			break
		}
		j += i
		widthStr := s[i+len(marker) : j]
		width, err := strconv.Atoi(widthStr)
		if err != nil || width < 0 {
			// Unrecognized — leave literal and continue past this brace.
			b.WriteString(s[:j+1])
			s = s[j+1:]
			continue
		}
		b.WriteString(s[:i])
		fmt.Fprintf(&b, "%0*d", width, instance)
		s = s[j+1:]
	}
	return b.String()
}

// cpeStackInputs is the bundle buildCPEStack needs to assemble one CPE.
type cpeStackInputs struct {
	id         string
	serial     string
	instance   int
	fleetCount int
	pool       *transport.Pool
	rngSource  *cperng.Source
	sched      *scheduler.Scheduler
	listener   *cr.Listener // may be nil
	logger     *slog.Logger
}

// buildCPEStack constructs one CPE: fresh tree (re-loaded from disk),
// stamped serial, transport, tracker, session, scheduler registration,
// CR listener registration, generator runner. The returned stack's
// genRunner is non-nil iff the profile declares generators.
func buildCPEStack(cfg cpeconfig.Config, in cpeStackInputs) (*cpeStack, error) {
	prof, err := paramtree.LoadProfile(cfg.ProfilePath)
	if err != nil {
		return nil, fmt.Errorf("reload profile: %w", err)
	}

	// Stamp the per-CPE serial. SetSystem bypasses Writable so a
	// read-only SerialNumber leaf still gets the per-CPE value.
	if stampErr := prof.Tree.SetSystem(prof.DeviceIDPaths.SerialNumber, in.serial); stampErr != nil {
		return nil, fmt.Errorf("stamp serial at %q: %w", prof.DeviceIDPaths.SerialNumber, stampErr)
	}

	// Substitute fleet placeholders in every leaf value. Lets operators
	// differentiate IPs, MACs, hostnames, IPv6 prefixes, custom IDs etc.
	// across the fleet either via inline forms ({cpe}, {cpe:hex:N},
	// {cpe:ipv4:CIDR}, ...) or named pools ({wan_ipv4}) declared in
	// fleet.pools.
	if subErr := applyFleetPlaceholders(prof.Tree, in.instance, in.id, prof.Fleet.Pools); subErr != nil {
		return nil, fmt.Errorf("fleet placeholder substitution: %w", subErr)
	}

	tt, err := transport.NewTransport(in.pool, transport.Config{
		ACSURL:   cfg.ACSURL,
		Username: cfg.ACSUsername,
		Password: cfg.ACSPassword,
		Timeout:  cfg.ACSTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("transport: %w", err)
	}

	tracker := cwmp.NewEventTracker(prof.InformParameters)

	builderOpts := inform.BuilderOptions{
		DeviceIDPaths: inform.DeviceIDPaths{
			Manufacturer: prof.DeviceIDPaths.Manufacturer,
			OUI:          prof.DeviceIDPaths.OUI,
			ProductClass: prof.DeviceIDPaths.ProductClass,
			SerialNumber: prof.DeviceIDPaths.SerialNumber,
		},
	}
	placeholder, err := inform.NewBuilder(prof.Tree, builderOpts)
	if err != nil {
		return nil, fmt.Errorf("inform builder: %w", err)
	}

	runOpts := &cwmp.RunSessionOptions{
		Tracker:       tracker,
		Tree:          prof.Tree,
		DeviceIDPaths: builderOpts.DeviceIDPaths,
	}
	sessionMu := &sync.Mutex{}

	scheduleTransfer := buildTransferScheduler(in.sched, in.id, tracker, prof.Transfer, runOpts, in.logger)

	factoryReset := func() error {
		fresh, loadErr := paramtree.LoadProfile(cfg.ProfilePath)
		if loadErr != nil {
			return fmt.Errorf("reload profile: %w", loadErr)
		}
		if resetErr := prof.Tree.Reset(fresh.Tree); resetErr != nil {
			return fmt.Errorf("tree reset: %w", resetErr)
		}
		// Re-stamp serial post-reset so factory reset doesn't collapse
		// the CPE back onto the template's base serial.
		if stampErr := prof.Tree.SetSystem(prof.DeviceIDPaths.SerialNumber, in.serial); stampErr != nil {
			return fmt.Errorf("re-stamp serial: %w", stampErr)
		}
		tracker.ResetBootstrap()
		return nil
	}

	// pendingCancels holds the cancel funcs for in-flight scheduled
	// reboot / factory-reset / usp-reboot deliveries. Both write paths
	// run under sessionMu (handler invocation holds it; scheduler.
	// ScheduleOnce re-acquires it before invoking its fn) so no extra
	// lock needed.
	pendingCancels := &pendingScheduledCancels{}

	var scheduleReboot handlers.RebootSchedule
	if prof.EventSchedule.RebootDelay > 0 {
		scheduleReboot = buildRebootScheduler(in.sched, in.id, tracker, prof.EventSchedule.RebootDelay, runOpts, pendingCancels, in.logger)
	}
	var scheduleFactoryReset handlers.FactoryResetSchedule
	if prof.EventSchedule.FactoryResetDelay > 0 {
		scheduleFactoryReset = buildFactoryResetScheduler(in.sched, in.id, prof.EventSchedule.FactoryResetDelay, runOpts, pendingCancels, in.logger)
	}

	hasScheduler := !prof.PeriodicInformPaths.IsZero()
	valueChange := func(path string) {
		tracker.RecordValueChange(path)
		if hasScheduler &&
			(path == prof.PeriodicInformPaths.Interval || path == prof.PeriodicInformPaths.Enable) {
			in.sched.OnIntervalChange(in.id)
		}
	}

	session, err := cwmp.NewSession(cwmp.SessionOptions{
		Transport: tt,
		Inform:    placeholder,
		Logger:    in.logger.With("cpe_id", in.id),
		Handlers: []cwmp.Handler{
			handlers.NewGetParameterValues(prof.Tree),
			handlers.NewGetParameterNames(prof.Tree),
			handlers.NewGetParameterAttributes(prof.Tree),
			handlers.NewSetParameterValues(prof.Tree, valueChange),
			handlers.NewSetParameterAttributes(prof.Tree),
			handlers.NewAddObject(prof.Tree),
			handlers.NewDeleteObject(prof.Tree),
			handlers.NewReboot(tracker, scheduleReboot),
			handlers.NewFactoryReset(factoryReset, scheduleFactoryReset),
			handlers.NewDownload(scheduleTransfer),
			handlers.NewUpload(scheduleTransfer),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	runOpts.Session = session

	if hasScheduler {
		// Set Notification=1 on the interval/enable leaves so SPV
		// triggers valueChange (most stacks ship them with
		// Notification=0 because the ACS is the writer).
		for _, p := range []string{prof.PeriodicInformPaths.Interval, prof.PeriodicInformPaths.Enable} {
			attrs, gerr := prof.Tree.GetAttributes(p)
			if gerr != nil {
				return nil, fmt.Errorf("get attributes %q: %w", p, gerr)
			}
			attrs.Notification = 1
			if serr := prof.Tree.SetAttributes(p, attrs); serr != nil {
				return nil, fmt.Errorf("set notification on %q: %w", p, serr)
			}
		}
		schedRNG := in.rngSource.ForCPE(in.id)
		cpeID := in.id
		logger := in.logger
		if rerr := in.sched.Schedule(scheduler.Registration{
			CPEID: cpeID,
			Tree:  prof.Tree,
			Paths: scheduler.PeriodicInformPaths{
				Interval: prof.PeriodicInformPaths.Interval,
				Enable:   prof.PeriodicInformPaths.Enable,
			},
			OnTick: func(_ context.Context) error {
				start := time.Now()
				if sessErr := cwmp.RunSession(context.Background(), *runOpts, cwmp.TriggerPeriodic); sessErr != nil {
					logger.Warn("periodic session failed",
						"cpe_id", cpeID,
						"duration", time.Since(start).String(),
						"err", sessErr.Error())
					return sessErr
				}
				logger.Info("periodic session completed",
					"cpe_id", cpeID,
					"duration", time.Since(start).String())
				return nil
			},
			RNG:       schedRNG,
			SessionMu: sessionMu,
			JitterPct: 0.10,
		}); rerr != nil {
			return nil, fmt.Errorf("scheduler.Schedule: %w", rerr)
		}
	}

	// Generators: per-CPE Runner with its own Tree + RNG.
	var genRunner *generators.Runner
	if len(prof.Generators) > 0 {
		gr, gerr := buildGenerators(prof.Generators, prof.Tree, in.rngSource.ForCPE(in.id+":generators"), in.logger.With("cpe_id", in.id))
		if gerr != nil {
			return nil, fmt.Errorf("generators: %w", gerr)
		}
		genRunner = gr
	}

	// CR listener registration (per-CPE path when count > 1).
	if in.listener != nil {
		if regErr := registerCREndpoint(in.listener, cfg, prof, in.id, in.fleetCount, runOpts, sessionMu, in.logger); regErr != nil {
			return nil, fmt.Errorf("register CR endpoint: %w", regErr)
		}
	}

	var uspAdapter mtp.Adapter
	var uspOpts *uspsession.Options
	if prof.USP.Enable {
		oui, gerr := prof.Tree.Get(prof.USP.EndpointID.OUIPath)
		if gerr != nil {
			return nil, fmt.Errorf("read usp OUI leaf %q: %w", prof.USP.EndpointID.OUIPath, gerr)
		}
		serial, gerr := prof.Tree.Get(prof.USP.EndpointID.SerialPath)
		if gerr != nil {
			return nil, fmt.Errorf("read usp Serial leaf %q: %w", prof.USP.EndpointID.SerialPath, gerr)
		}
		agentEID := identity.EID(oui.Raw, serial.Raw)
		if vErr := identity.Validate(agentEID); vErr != nil {
			return nil, fmt.Errorf("agent EID %q invalid: %w", agentEID, vErr)
		}
		adapter, mErr := mqttmtp.New(mqttmtp.Options{
			BrokerHost:       prof.USP.Broker.Address,
			BrokerPort:       prof.USP.Broker.Port,
			Username:         prof.USP.Broker.Username,
			Password:         prof.USP.Broker.Password,
			EndpointID:       agentEID,
			KeepAlive:        time.Duration(prof.USP.Broker.KeepAliveSeconds) * time.Second,
			CleanSession:     prof.USP.Broker.CleanSession,
			Logger:           in.logger.With("cpe_id", in.id, "role", "usp"),
		})
		if mErr != nil {
			return nil, fmt.Errorf("usp mqtt adapter: %w", mErr)
		}
		uspAdapter = adapter
		uspLogger := in.logger.With("cpe_id", in.id, "role", "usp")
		objectCreate := buildUSPObjectCreate(adapter, agentEID, prof.USP.ControllerEndpointID, uspLogger)
		objectDelete := buildUSPObjectDelete(adapter, agentEID, prof.USP.ControllerEndpointID, uspLogger)
		uspReboot := buildUSPRebootCallback(in.sched, in.id, prof.Tree, prof.EventSchedule.RebootDelay,
			adapter, agentEID, prof.USP.ControllerEndpointID, pendingCancels, uspLogger)

		uspOpts = &uspsession.Options{
			Tree:          prof.Tree,
			Adapter:       adapter,
			AgentEID:      agentEID,
			ControllerEID: prof.USP.ControllerEndpointID,
			OnBoardBuilder: &notify.OnBoardRequestBuilder{
				OUIPath:    prof.USP.EndpointID.OUIPath,
				SerialPath: prof.USP.EndpointID.SerialPath,
			},
			BootEventBuilder: &notify.BootEventBuilder{},
			Handlers: []uspsession.Handler{
				usphandlers.NewGet(prof.Tree),
				usphandlers.NewSet(prof.Tree, valueChange),
				usphandlers.NewAdd(prof.Tree, prof.UniqueKeys, valueChange, objectCreate),
				usphandlers.NewDelete(prof.Tree, objectDelete),
				usphandlers.NewOperate(uspReboot),
			},
			Logger: uspLogger,
		}
	}

	return &cpeStack{
		id:           in.id,
		serial:       in.serial,
		tree:         prof.Tree,
		tracker:      tracker,
		transport:    tt,
		session:      session,
		sessionMu:    sessionMu,
		runOpts:      runOpts,
		genRunner:    genRunner,
		hasScheduler: hasScheduler,
		uspAdapter:   uspAdapter,
		uspOpts:      uspOpts,
	}, nil
}

// bootstrapAll fires the startup Inform for every CPE in parallel.
// Returns nil if every CPE succeeds; the first error otherwise. Failed
// CPEs are logged with their cpe_id so an operator can identify which
// one of N misbehaved.
//
// bootDelay > 0 delays each per-CPE bootstrap by exactly that wall-
// clock duration (real time.Sleep), modelling a CPE that takes time
// to reach the ACS after process start. The fleet still bootstraps in
// parallel — every CPE waits the same delay independently.
func bootstrapAll(ctx context.Context, stacks []*cpeStack, bootDelay time.Duration, logger *slog.Logger) error {
	if len(stacks) == 0 {
		return nil
	}
	type result struct {
		id  string
		err error
	}
	results := make(chan result, len(stacks))
	var wg sync.WaitGroup
	for _, st := range stacks {
		st := st
		wg.Add(1)
		go func() {
			defer wg.Done()
			if bootDelay > 0 {
				select {
				case <-time.After(bootDelay):
				case <-ctx.Done():
					results <- result{id: st.id, err: ctx.Err()}
					return
				}
			}
			start := time.Now()
			if err := cwmp.RunSession(ctx, *st.runOpts, cwmp.TriggerStartup); err != nil {
				logger.Info("bootstrap session failed",
					"cpe_id", st.id, "serial", st.serial,
					"duration", time.Since(start).String(),
					"err", err.Error())
				results <- result{id: st.id, err: err}
				return
			}
			logger.Info("bootstrap session completed",
				"cpe_id", st.id, "serial", st.serial,
				"duration", time.Since(start).String())
			results <- result{id: st.id}
		}()
	}
	wg.Wait()
	close(results)
	var firstErr error
	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = fmt.Errorf("cpe %s: %w", r.id, r.err)
		}
	}
	return firstErr
}

// registerCREndpoint registers one CPE's connection-request endpoint
// with the shared listener. When fleetCount > 1 the path is suffixed
// with /<cpeID> so the ACS can route to a specific CPE; for fleet
// count == 1 the path is used as-is to keep single-CPE deployments'
// URL shape unchanged.
//
// The CRPublishPath leaf in tree is validated as writable, and the
// resolved URL is written into it via Tree.Set so the next Inform
// reports the correct ConnectionRequestURL.
func registerCREndpoint(listener *cr.Listener, cfg cpeconfig.Config, prof *paramtree.Profile, cpeID string, fleetCount int, runOpts *cwmp.RunSessionOptions, sessionMu *sync.Mutex, logger *slog.Logger) error {
	tree := prof.Tree

	cur, err := tree.Get(cfg.CRPublishPath)
	if err != nil {
		return fmt.Errorf("cr-publish-path %q not found in profile: %w", cfg.CRPublishPath, err)
	}
	if !cur.Writable {
		return fmt.Errorf("cr-publish-path %q is not writable in profile", cfg.CRPublishPath)
	}

	path := cfg.CRPath
	if fleetCount > 1 {
		// Per-CPE suffix so the listener routes inbound CRs to the
		// right session. Single-CPE deployments keep cfg.CRPath
		// unchanged for backward compat.
		if !strings.HasSuffix(path, "/") {
			path += "/"
		}
		path += cpeID
	}

	onRequest := func(_ context.Context) {
		if !sessionMu.TryLock() {
			logger.Debug("connection request dropped: session already in progress", "cpe_id", cpeID)
			return
		}
		go func() {
			defer sessionMu.Unlock()
			start := time.Now()
			if sessErr := cwmp.RunSession(context.Background(), *runOpts, cwmp.TriggerConnectionRequest); sessErr != nil {
				logger.Warn("CR session failed",
					"cpe_id", cpeID,
					"duration", time.Since(start).String(),
					"err", sessErr.Error())
				return
			}
			logger.Info("CR session completed",
				"cpe_id", cpeID,
				"duration", time.Since(start).String())
		}()
	}

	authn, err := buildCRAuthenticator(prof.ConnectionRequest, tree)
	if err != nil {
		return err
	}

	if err := listener.Register(cr.Endpoint{
		Path:      path,
		OnRequest: onRequest,
		Auth:      authn,
		Throttle:  prof.ConnectionRequest.ThrottleWindow,
	}); err != nil {
		return fmt.Errorf("register CR endpoint %q: %w", path, err)
	}

	url := listener.URL(path)
	if err := tree.Set(cfg.CRPublishPath, paramtree.Value{
		Type: cur.Type, Raw: url, Writable: cur.Writable,
	}); err != nil {
		return fmt.Errorf("publish CR URL into tree at %q: %w", cfg.CRPublishPath, err)
	}
	logger.Info("connection-request URL published",
		"cpe_id", cpeID, "path", cfg.CRPublishPath, "url", url)
	return nil
}

// buildCRAuthenticator constructs the per-Endpoint Authenticator for
// the CR listener based on the profile's connectionRequest block.
// Returns nil when scheme is "" (matches #28's always-permit default).
//
// No defaults are filled in here — the profile loader has already
// validated that realm / usernameParameter / passwordParameter are
// non-empty when scheme != "" (CLAUDE.md anchor #3).
func buildCRAuthenticator(cfg paramtree.ConnectionRequestConfig, tree *paramtree.Tree) (cr.Authenticator, error) {
	if cfg.Scheme == "" {
		return nil, nil
	}
	lookup := func() (string, string) {
		u, _ := tree.Get(cfg.UsernameParameter)
		p, _ := tree.Get(cfg.PasswordParameter)
		return u.Raw, p.Raw
	}
	switch cfg.Scheme {
	case "basic":
		return cr.BasicAuth(cfg.Realm, lookup), nil
	case "digest":
		return cr.DigestAuth(cr.DigestOptions{Realm: cfg.Realm, Lookup: lookup}), nil
	}
	return nil, fmt.Errorf("connectionRequest.scheme %q unsupported", cfg.Scheme)
}

// buildTransferScheduler returns a handlers.Schedule that, on each
// accepted Download/Upload, registers a one-shot with the scheduler.
// When the one-shot fires the closure:
//
//  1. Looks up the per-FileType fault injection in the profile (none
//     → FaultCode=0).
//  2. Queues the M-event + TransferComplete record on the tracker.
//  3. Fires a TriggerPeriodic session that delivers the
//     TransferComplete to the ACS.
//
// The previous implementation spawned a free goroutine per Pending;
// folding it into the scheduler subsumes the #27 flagged Phase 4
// debt and ensures TransferComplete delivery acquires the same
// SessionMu as periodic ticks and CR sessions.
//
// runOpts is held by pointer so the scheduler picks up the Session
// once cmd/cpe-sim's main has finished constructing it.
func buildTransferScheduler(sched *scheduler.Scheduler, cpeID string, tracker *cwmp.EventTracker, cfg paramtree.TransferConfig, runOpts *cwmp.RunSessionOptions, logger *slog.Logger) handlers.Schedule {
	defaultDelay := cfg.DefaultDelay
	if defaultDelay <= 0 {
		defaultDelay = 5 * time.Second
	}
	return func(p handlers.Pending) {
		delay := defaultDelay + time.Duration(p.DelaySeconds)*time.Second
		fault := lookupTransferFault(cfg, p.FileType)
		logger.Debug("transfer scheduler enqueue",
			"cpe_id", cpeID,
			"command_key", p.CommandKey,
			"is_download", p.IsDownload,
			"delay", delay.String(),
			"fault_code", fault.Code)

		_ = sched.ScheduleOnce(cpeID, delay, func(_ context.Context) error {
			complete := transfer.Complete{
				CommandKey:   p.CommandKey,
				FaultCode:    fault.Code,
				FaultString:  fault.String,
				StartTime:    p.StartTime,
				CompleteTime: time.Now().UTC(),
			}
			if p.IsDownload {
				tracker.QueueMethodDownload(p.CommandKey)
			} else {
				tracker.QueueMethodUpload(p.CommandKey)
			}
			tracker.QueueTransferComplete(complete)
			if runOpts.Session == nil {
				logger.Warn("transfer scheduler: session not yet constructed",
					"cpe_id", cpeID, "command_key", p.CommandKey)
				return nil
			}
			start := time.Now()
			if err := cwmp.RunSession(context.Background(), *runOpts, cwmp.TriggerPeriodic); err != nil {
				logger.Warn("transfer-complete session failed",
					"cpe_id", cpeID,
					"command_key", p.CommandKey,
					"duration", time.Since(start).String(),
					"err", err.Error())
				return err
			}
			logger.Info("transfer-complete session delivered",
				"cpe_id", cpeID,
				"command_key", p.CommandKey,
				"is_download", p.IsDownload,
				"fault_code", complete.FaultCode,
				"duration", time.Since(start).String())
			return nil
		})
	}
}

// pendingScheduledCancels is the per-CPE holder for in-flight
// scheduled-reboot / scheduled-factory-reset cancel funcs. The fields
// are read/written under the per-CPE SessionMu (handler invocation
// holds it; scheduler.ScheduleOnce re-acquires it before invoking the
// fired fn). Repeat scheduling supersedes the previous in-flight
// schedule rather than queuing two.
type pendingScheduledCancels = struct {
	reboot       func()
	factoryReset func()
	uspReboot    func()
}

// buildRebootScheduler returns a handlers.RebootSchedule that defers
// the post-Reboot effects by delay. Each call:
//
//  1. Cancels any in-flight scheduled reboot for this CPE (supersede).
//  2. Registers a new scheduler.ScheduleOnce; the fn (when fired)
//     queues "M Reboot" on the tracker and runs a TriggerStartup
//     session so the ACS sees [1 BOOT, M Reboot] — the wire shape a
//     real CPE produces after rebooting.
//
// runOpts is held by pointer so the scheduler picks up the Session
// once cmd/cpe-sim's main has finished constructing it (mirrors the
// transfer scheduler).
func buildRebootScheduler(sched *scheduler.Scheduler, cpeID string, tracker *cwmp.EventTracker, delay time.Duration, runOpts *cwmp.RunSessionOptions, cancels *pendingScheduledCancels, logger *slog.Logger) handlers.RebootSchedule {
	return func(commandKey string) {
		if cancels.reboot != nil {
			logger.Debug("scheduled reboot superseded by new RPC", "cpe_id", cpeID)
			cancels.reboot()
			cancels.reboot = nil
		}
		logger.Debug("scheduled reboot enqueued",
			"cpe_id", cpeID, "command_key", commandKey, "delay", delay.String())
		cancels.reboot = sched.ScheduleOnce(cpeID, delay, func(_ context.Context) error {
			cancels.reboot = nil
			tracker.QueueMethodReboot(commandKey)
			if runOpts.Session == nil {
				logger.Warn("scheduled reboot: session not yet constructed",
					"cpe_id", cpeID, "command_key", commandKey)
				return nil
			}
			start := time.Now()
			if err := cwmp.RunSession(context.Background(), *runOpts, cwmp.TriggerStartup); err != nil {
				logger.Warn("scheduled reboot session failed",
					"cpe_id", cpeID, "command_key", commandKey,
					"duration", time.Since(start).String(),
					"err", err.Error())
				return err
			}
			logger.Info("scheduled reboot session delivered",
				"cpe_id", cpeID, "command_key", commandKey,
				"duration", time.Since(start).String())
			return nil
		})
	}
}

// buildFactoryResetScheduler returns a handlers.FactoryResetSchedule
// that defers onReset by delay. The fired fn invokes onReset (logs
// any error — cannot surface to the ACS since the FactoryResetResponse
// has already been sent) and runs a TriggerStartup session so the ACS
// sees [1 BOOT, 0 BOOTSTRAP] — the wire shape a real CPE produces
// after a factory reset (BOOTSTRAP re-armed by ResetBootstrap inside
// onReset).
func buildFactoryResetScheduler(sched *scheduler.Scheduler, cpeID string, delay time.Duration, runOpts *cwmp.RunSessionOptions, cancels *pendingScheduledCancels, logger *slog.Logger) handlers.FactoryResetSchedule {
	return func(onReset func() error) {
		if cancels.factoryReset != nil {
			logger.Debug("scheduled factory reset superseded by new RPC", "cpe_id", cpeID)
			cancels.factoryReset()
			cancels.factoryReset = nil
		}
		logger.Debug("scheduled factory reset enqueued",
			"cpe_id", cpeID, "delay", delay.String())
		cancels.factoryReset = sched.ScheduleOnce(cpeID, delay, func(_ context.Context) error {
			cancels.factoryReset = nil
			if onReset != nil {
				if err := onReset(); err != nil {
					logger.Warn("scheduled factory reset onReset failed",
						"cpe_id", cpeID, "err", err.Error())
				}
			}
			if runOpts.Session == nil {
				logger.Warn("scheduled factory reset: session not yet constructed",
					"cpe_id", cpeID)
				return nil
			}
			start := time.Now()
			if err := cwmp.RunSession(context.Background(), *runOpts, cwmp.TriggerStartup); err != nil {
				logger.Warn("scheduled factory reset session failed",
					"cpe_id", cpeID,
					"duration", time.Since(start).String(),
					"err", err.Error())
				return err
			}
			logger.Info("scheduled factory reset session delivered",
				"cpe_id", cpeID,
				"duration", time.Since(start).String())
			return nil
		})
	}
}

// buildGenerators walks prof.Generators and constructs a runner with
// one Generator per entry. Switches on cfg.Type — only "counter" is
// supported in v0; future stories add drift / enum / timestamp.
func buildGenerators(cfgs []paramtree.GeneratorConfig, tree *paramtree.Tree, rng *rand.Rand, logger *slog.Logger) (*generators.Runner, error) {
	r, err := generators.NewRunner(generators.RunnerOptions{
		Logger: logger,
		Tree:   tree,
		RNG:    rng,
	})
	if err != nil {
		return nil, err
	}
	for _, cfg := range cfgs {
		var gen generators.Generator
		switch cfg.Type {
		case "counter":
			if cfg.Counter == nil {
				return nil, fmt.Errorf("generator %q: counter block missing", cfg.Path)
			}
			gen, err = generators.NewCounter(generators.CounterConfig{
				Path:   cfg.Path,
				Min:    cfg.Counter.Min,
				Max:    cfg.Counter.Max,
				Step:   cfg.Counter.Step,
				Jitter: cfg.Counter.Jitter,
			})
		case "drift":
			if cfg.Drift == nil {
				return nil, fmt.Errorf("generator %q: drift block missing", cfg.Path)
			}
			gen, err = generators.NewDrift(generators.DriftConfig{
				Path:    cfg.Path,
				Min:     cfg.Drift.Min,
				Max:     cfg.Drift.Max,
				StepMax: cfg.Drift.StepMax,
			})
		case "enum":
			if cfg.Enum == nil {
				return nil, fmt.Errorf("generator %q: enum block missing", cfg.Path)
			}
			gen, err = generators.NewEnum(generators.EnumConfig{
				Path:   cfg.Path,
				Values: cfg.Enum.Values,
				Mode:   cfg.Enum.Mode,
			})
		case "uptime":
			gen, err = generators.NewTimestamp(generators.TimestampConfig{
				Path: cfg.Path,
				Kind: generators.TimestampUptime,
			})
		case "wallclock":
			gen, err = generators.NewTimestamp(generators.TimestampConfig{
				Path: cfg.Path,
				Kind: generators.TimestampWallclock,
			})
		default:
			return nil, fmt.Errorf("generator %q: type %q unsupported", cfg.Path, cfg.Type)
		}
		if err != nil {
			return nil, fmt.Errorf("generator %q: %w", cfg.Path, err)
		}
		if err := r.Add(gen, cfg.Interval); err != nil {
			return nil, fmt.Errorf("generator %q: %w", cfg.Path, err)
		}
	}
	return r, nil
}

// lookupTransferFault returns the fault to inject for fileType, or
// the zero-value (success) if no entry matches.
func lookupTransferFault(cfg paramtree.TransferConfig, fileType string) paramtree.TransferFault {
	if cfg.Faults == nil {
		return paramtree.TransferFault{}
	}
	if f, ok := cfg.Faults[fileType]; ok {
		return f
	}
	return paramtree.TransferFault{}
}

func hasVersionFlag(args []string) bool {
	for _, a := range args {
		if a == "--version" || a == "-version" {
			return true
		}
	}
	return false
}

func buildUSPObjectCreate(adapter mtp.Adapter, agentEID, controllerEID string, logger *slog.Logger) func(string, map[string]string) {
	builder := &notify.ObjectCreationBuilder{}
	return func(objPath string, uniqueKeys map[string]string) {
		msg, err := builder.Build(objPath, uniqueKeys)
		if err != nil {
			logger.Warn("usp object-creation build failed", "obj_path", objPath, "err", err.Error())
			return
		}
		wire, err := uspcodec.WrapMessage(msg, agentEID, controllerEID)
		if err != nil {
			logger.Warn("usp object-creation wrap failed", "err", err.Error())
			return
		}
		if err := adapter.Send(context.Background(), wire); err != nil {
			logger.Warn("usp object-creation send failed", "err", err.Error())
			return
		}
		logger.Info("usp ObjectCreation notify sent", "obj_path", objPath, "unique_keys", uniqueKeys)
	}
}

func buildUSPObjectDelete(adapter mtp.Adapter, agentEID, controllerEID string, logger *slog.Logger) func(string) {
	builder := &notify.ObjectDeletionBuilder{}
	return func(objPath string) {
		msg, err := builder.Build(objPath)
		if err != nil {
			logger.Warn("usp object-deletion build failed", "obj_path", objPath, "err", err.Error())
			return
		}
		wire, err := uspcodec.WrapMessage(msg, agentEID, controllerEID)
		if err != nil {
			logger.Warn("usp object-deletion wrap failed", "err", err.Error())
			return
		}
		if err := adapter.Send(context.Background(), wire); err != nil {
			logger.Warn("usp object-deletion send failed", "err", err.Error())
			return
		}
		logger.Info("usp ObjectDeletion notify sent", "obj_path", objPath)
	}
}

func buildUSPRebootCallback(sched *scheduler.Scheduler, cpeID string, tree *paramtree.Tree, delay time.Duration, adapter mtp.Adapter, agentEID, controllerEID string, cancels *pendingScheduledCancels, logger *slog.Logger) func() {
	bootBuilder := &notify.BootEventBuilder{}
	return func() {
		if err := tree.SetSystem(uspsession.RebootCausePath, uspsession.RebootCauseLocalBoot); err != nil {
			logger.Warn("usp reboot: flip Internal.Reboot.Cause failed", "err", err.Error())
		}
		if cancels.uspReboot != nil {
			logger.Debug("usp scheduled reboot superseded by new RPC", "cpe_id", cpeID)
			cancels.uspReboot()
			cancels.uspReboot = nil
		}
		logger.Debug("usp reboot scheduled", "cpe_id", cpeID, "delay", delay.String())
		cancels.uspReboot = sched.ScheduleOnce(cpeID+":usp-reboot", delay, func(_ context.Context) error {
			msg, err := bootBuilder.Build(tree)
			if err != nil {
				logger.Warn("usp boot event build failed", "err", err.Error())
				return err
			}
			wire, err := uspcodec.WrapMessage(msg, agentEID, controllerEID)
			if err != nil {
				logger.Warn("usp boot event wrap failed", "err", err.Error())
				return err
			}
			if err := adapter.Send(context.Background(), wire); err != nil {
				logger.Warn("usp boot event send failed", "err", err.Error())
				return err
			}
			logger.Info("usp Event{Boot!} notify sent after reboot", "cpe_id", cpeID)
			return nil
		})
	}
}
