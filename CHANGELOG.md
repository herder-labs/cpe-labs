# Changelog

## [0.1.1](https://github.com/herder-labs/cpe-labs/compare/v0.1.0...v0.1.1) (2026-05-15)


### Features

* **acceptance:** CWMP CR roundtrip scenario ([#17](https://github.com/herder-labs/cpe-labs/issues/17)) ([#19](https://github.com/herder-labs/cpe-labs/issues/19)) ([9480447](https://github.com/herder-labs/cpe-labs/commit/9480447951e07e8ab7ed12aa5b00e4f1a17f278c))
* **acceptance:** CWMP periodic Inform scenario ([#16](https://github.com/herder-labs/cpe-labs/issues/16)) ([#18](https://github.com/herder-labs/cpe-labs/issues/18)) ([541ec72](https://github.com/herder-labs/cpe-labs/commit/541ec72cf4207d1947eef3900f0182dbc613187e))
* **acceptance:** USP first-contact scenario + USP harness primitives ([#15](https://github.com/herder-labs/cpe-labs/issues/15)) ([#17](https://github.com/herder-labs/cpe-labs/issues/17)) ([25d0dcd](https://github.com/herder-labs/cpe-labs/commit/25d0dcd32731d2f381bf5c8fd7a88dcd36ee899d))
* **acceptance:** USP Operate(Reboot) scenario ([#18](https://github.com/herder-labs/cpe-labs/issues/18)) ([#20](https://github.com/herder-labs/cpe-labs/issues/20)) ([4ccb4be](https://github.com/herder-labs/cpe-labs/commit/4ccb4beeeb5e1f76ae57624885164e351a424220))
* **acceptance:** USP Set + ValueChange Notify scenario ([#19](https://github.com/herder-labs/cpe-labs/issues/19)) ([#21](https://github.com/herder-labs/cpe-labs/issues/21)) ([aa73f35](https://github.com/herder-labs/cpe-labs/commit/aa73f35e417f3b96ea03887792d5139d8744d4f5))
* **acceptance:** wire-format acceptance suite ([#13](https://github.com/herder-labs/cpe-labs/issues/13)) ([295027f](https://github.com/herder-labs/cpe-labs/commit/295027f22f4b1910851d9667ea0b94bb570f8506))
* **acceptance:** wire-format acceptance suite (harness + first scenario) ([41f3d24](https://github.com/herder-labs/cpe-labs/commit/41f3d24579fa852595cd7a605c2c85569ec298ec))
* **admin:** HTTP introspection server (list, detail, force-CR, fault-inject) ([7ec2512](https://github.com/herder-labs/cpe-labs/commit/7ec25122f169b66cb4c022b1f43efaec23432b37))
* **behavior:** client fabricators (WiFi station + LAN host churn) — closes Phase 4 ([6d03189](https://github.com/herder-labs/cpe-labs/commit/6d0318978f7f664ecf58192dd61b8a0bb9d59188))
* **clients:** client fabricator subsystem + runner + cmd/cpe-sim wiring ([0dfabcb](https://github.com/herder-labs/cpe-labs/commit/0dfabcbb58c4a20b43cd22d0afd8abcfde2246e0))
* **cpe-sim:** wire metrics/admin listener and per-CPE lifecycle ([b5fe16b](https://github.com/herder-labs/cpe-labs/commit/b5fe16b5425449327ee7cbb6d69c75ca78656025))
* initial release ([5d28b90](https://github.com/herder-labs/cpe-labs/commit/5d28b90c22ebd4fa4d2867ce7beccc4e222f6b22))
* **metrics:** instrument cwmp/usp state owners ([db467a7](https://github.com/herder-labs/cpe-labs/commit/db467a7aac26db0311dd028dbb7f22141f12dd77))
* **metrics:** Prometheus registry and process collector ([79a20be](https://github.com/herder-labs/cpe-labs/commit/79a20be8c6cb428f09c9dfc91f1dda9a14d19915))
* **observability:** Prometheus /metrics, admin endpoints, bench harness ([c3b063d](https://github.com/herder-labs/cpe-labs/commit/c3b063df25d21db4f3d3f32ec0491e4c33ea7703))
* **paramtree:** auto-install Device.LocalAgent.Subscription table on USP-enable ([6872f49](https://github.com/herder-labs/cpe-labs/commit/6872f49bbc2183f8314c75f8119185af49ccbcfd))
* **paramtree:** clients: profile schema for client fabricators ([8c7ab87](https://github.com/herder-labs/cpe-labs/commit/8c7ab872d1a2df560d0b0d35b3980268a2abe930))
* **paramtree:** objects[].uniqueKeys schema + Profile.UniqueKeys map ([ddfc41d](https://github.com/herder-labs/cpe-labs/commit/ddfc41da2e372bc9ea708df657ea3fa41a647358))
* **paramtree:** Tree.OnWrite callbacks + Tree.IsAddDeletable accessor ([66cb5db](https://github.com/herder-labs/cpe-labs/commit/66cb5db34c60444ee00ca18d88f26d62bb8097f2))
* **profiles, docs:** reference clients: blocks + docs/reference ([1929d48](https://github.com/herder-labs/cpe-labs/commit/1929d48b2ff60e0b644e6a6d04aec2c5cdc100ad))
* **profiles:** TR-181 spec coverage + per-extender backhaul RSSI demo ([f833763](https://github.com/herder-labs/cpe-labs/commit/f833763fc63c15a292f2e573526a0e18eb83b88e))
* **profiles:** TR-181 spec coverage + per-extender backhaul RSSI demo ([50a9b79](https://github.com/herder-labs/cpe-labs/commit/50a9b797e5ea7a763196e958c8615202a49dc445))
* **profiles:** uniform AssociatedDevice leaves + missing radio/SSID/AP fields ([f7e491c](https://github.com/herder-labs/cpe-labs/commit/f7e491c294072cc24d5fe39db9ba6e143bf59c1d))
* **profiles:** uniform AssociatedDevice leaves + missing radio/SSID/AP fields ([2020319](https://github.com/herder-labs/cpe-labs/commit/2020319509d0915d73b9504bb8ae01e20bee7325))
* **profiles:** uniqueKeys on Device.WiFi.{Radio,SSID,AccessPoint} ([3bad49a](https://github.com/herder-labs/cpe-labs/commit/3bad49aaae2fb37afc21534190a1b9284df778e3))
* **usp:** Add request handler with autonomous ObjectCreation Notify ([59eeaad](https://github.com/herder-labs/cpe-labs/commit/59eeaad4e980e2ce201a99cb506a9987872fc35d))
* **usp:** cmd/cpe-sim wires Subscription evaluator + Bundle 2 handlers ([a65fe1c](https://github.com/herder-labs/cpe-labs/commit/a65fe1cfac1e6b281d98f9ef08b1f6891b04fa8e))
* **usp:** Delete request handler with autonomous ObjectDeletion Notify ([15b1fa9](https://github.com/herder-labs/cpe-labs/commit/15b1fa9260dbcc42007fd3ebbae6be03b523f876))
* **usp:** EID identity helpers (EID, Build, Validate) ([e78c56d](https://github.com/herder-labs/cpe-labs/commit/e78c56d71908d5dd06bad9fac38ecbfcf57b65fd))
* **usp:** Get request handler ([cc293b5](https://github.com/herder-labs/cpe-labs/commit/cc293b5d9975d084ad89f157d8e9d05508f86fb1))
* **usp:** GetInstances request handler ([05efc12](https://github.com/herder-labs/cpe-labs/commit/05efc127a0c53bd86e4a489b22c619fafa089dcb))
* **usp:** GetSupportedDM request handler ([8ba4e52](https://github.com/herder-labs/cpe-labs/commit/8ba4e52e749e9b664117549815ed2aea07336c94))
* **usp:** Handler interface + dispatch table in session ([b277553](https://github.com/herder-labs/cpe-labs/commit/b277553597ad2f9c130e9c6ce2efdb68689fa74c))
* **usp:** HMAC-PSK broker auth + per-EID topics + bounded connect retry ([#23](https://github.com/herder-labs/cpe-labs/issues/23)) ([93c2a64](https://github.com/herder-labs/cpe-labs/commit/93c2a6408ba6683f81f90314b5da317d4591c7bb))
* **usp:** minimal session loop with first-contact gate ([841bcbb](https://github.com/herder-labs/cpe-labs/commit/841bcbb1049cc6521f36c3ec92915fbf85f1e9e2))
* **usp:** MQTT 3.1.1 adapter (paho-mqtt) with embedded-broker tests ([e2b90e1](https://github.com/herder-labs/cpe-labs/commit/e2b90e1e08af3a2a3374de81e76aa9be34c8cce4))
* **usp:** MTP adapter interface + topic helpers ([ad52dc2](https://github.com/herder-labs/cpe-labs/commit/ad52dc268e1751acad15dcef53c9c35a43240bb5))
* **usp:** ObjectCreation and ObjectDeletion Notify builders ([7f26b68](https://github.com/herder-labs/cpe-labs/commit/7f26b68a5bc2dc106a71b78287215c925665bcc9))
* **usp:** OnBoardRequest and Boot! Event Notify builders ([460e433](https://github.com/herder-labs/cpe-labs/commit/460e433479d93534f78b79fa7a7ea796fbe20cdd))
* **usp:** Operate handler with Device.Reboot() support ([7679420](https://github.com/herder-labs/cpe-labs/commit/7679420c0318c8b5451b42a3e6ed72455ea15b1e))
* **usp:** Phase 3 close — Bundle 1 dispatch/handlers + Bundle 2 GetInstances/GetSupportedDM/Subscriptions ([0327377](https://github.com/herder-labs/cpe-labs/commit/032737710f6caccc44c87f4c8afe25ce8e3271ed))
* **usp:** profile usp: block + Internal.Reboot.Cause system leaf ([146bc56](https://github.com/herder-labs/cpe-labs/commit/146bc563cf40f9e34e681af4c30903ddaaf6b741))
* **usp:** proto + MQTT 3.1.1 MTP + OnBoardRequest first contact ([7dd047c](https://github.com/herder-labs/cpe-labs/commit/7dd047c8c0db0309ec05f8b01e870c8b67ae068a))
* **usp:** Record/Msg framing helpers (WrapMessage, UnwrapRecord) ([2a57700](https://github.com/herder-labs/cpe-labs/commit/2a57700de9b61fe162c7f3428a727bfb1ba178e0))
* **usp:** Set request handler with allow_partial:false rollback ([713ea49](https://github.com/herder-labs/cpe-labs/commit/713ea49b2ca990432f6ba489c063229e6560024a))
* **usp:** Subscription evaluator with autonomous Notify dispatch ([a531586](https://github.com/herder-labs/cpe-labs/commit/a531586a656137d2a540008f1164e03c4dca4aeb))
* **usp:** ValueChange Notify builder ([88894d3](https://github.com/herder-labs/cpe-labs/commit/88894d31eaa5239969bd3c0edced5614b4a906f4))
* **usp:** vendor BBF TR-369 proto schemas + proto-gen target ([dc96b1e](https://github.com/herder-labs/cpe-labs/commit/dc96b1ef0289a5e77cb79319cfecd49f37226700))
* **usp:** wire cpe-sim per-CPE USP session + OnBoardRequest integration test ([fc588c4](https://github.com/herder-labs/cpe-labs/commit/fc588c4b1cf36f09c35c55a455cbcf77aadb069e))


### Bug Fixes

* **cpe-sim:** mutex-protect pendingScheduledCancels ([9ca5356](https://github.com/herder-labs/cpe-labs/commit/9ca5356070329af676cc474fcb29a19904605b18)), closes [#14](https://github.com/herder-labs/cpe-labs/issues/14)
* **cpe-sim:** mutex-protect pendingScheduledCancels ([#14](https://github.com/herder-labs/cpe-labs/issues/14)) ([2c5b0d9](https://github.com/herder-labs/cpe-labs/commit/2c5b0d987f6b8008d109b46ec95cc885bf1f80b2))
* **cpe-sim:** publish CR URL after listener.Start ([#20](https://github.com/herder-labs/cpe-labs/issues/20)) ([#22](https://github.com/herder-labs/cpe-labs/issues/22)) ([96a11b0](https://github.com/herder-labs/cpe-labs/commit/96a11b0ae8b2896852e5c4a866dc363918f9c4b6))
* **docs:** site_url to custom domain + mobile button stacking ([b03a832](https://github.com/herder-labs/cpe-labs/commit/b03a83223f6f7b9460e0442478311e192b1b45e1))
* **tr181:** canonical MAC paths + valid 6-byte MACAddress templates ([7b4c760](https://github.com/herder-labs/cpe-labs/commit/7b4c7603940e829d91bf3b6a3ed295f875612b08))
* **tr181:** canonical MAC paths + valid 6-byte MACAddress templates ([a347a7e](https://github.com/herder-labs/cpe-labs/commit/a347a7ecc56c702b7c9231ed9819a4bfc7009e7a))
* **tr181:** hosts.yaml PhysAddress emits valid 6-byte MAC ([70da0d1](https://github.com/herder-labs/cpe-labs/commit/70da0d1eed5f77b39a9f0b0504839faf6f088681))
* **tr181:** hosts.yaml PhysAddress emits valid 6-byte MAC ([c91c634](https://github.com/herder-labs/cpe-labs/commit/c91c634927cb5807de07a4479ff492740e68b6b9))
* **usp/subscription:** Periodic period-change re-arm; close cancel/rearm race ([28a8c63](https://github.com/herder-labs/cpe-labs/commit/28a8c63c896b5225c8a4018967790e90f9d546cf))
* **usp/subscription:** register Tree.OnWrite once; gate via enabled flag ([1565249](https://github.com/herder-labs/cpe-labs/commit/15652490d5910df0e08a535b737fcec257883e05))
* **usp/subscription:** runtime reconciliation hardening ([#12](https://github.com/herder-labs/cpe-labs/issues/12)) ([c8407c1](https://github.com/herder-labs/cpe-labs/commit/c8407c10cd9d281de58018060a2dded74a055675))
* **usp/subscription:** warn on malformed rows; drop dead filter branch ([e34d2a0](https://github.com/herder-labs/cpe-labs/commit/e34d2a0ca5b2efc5d2d3adc84280b8f94250e0c7))
* **usp:** publish to controller with /reply-to= suffix per TR-369 R-MQTT.24 ([f346a60](https://github.com/herder-labs/cpe-labs/commit/f346a60e6ed810863b1bb81485994936849b30bd))


### Documentation

* **acceptance:** README + CI integration ([00dfa95](https://github.com/herder-labs/cpe-labs/commit/00dfa95149c48287ed98339304ffb9c4dc47f4b7))
* **observability:** /metrics scrape, admin endpoints, bench-output guide ([6f7d774](https://github.com/herder-labs/cpe-labs/commit/6f7d774fdab01fb8dc04388dea71c6617d626c21))
* **readme:** badges + custom-domain docs link [skip ci] ([8689f1a](https://github.com/herder-labs/cpe-labs/commit/8689f1a2dcb52a9c892d569db631175d6fd87598))
* rename OpenACS → Herder (project name) ([d882a2d](https://github.com/herder-labs/cpe-labs/commit/d882a2d626b77b488ccf618898d5a210190c0479))
* **usp:** close forward-looking sections; document Subscription evaluator ([086dcf1](https://github.com/herder-labs/cpe-labs/commit/086dcf1cb3d090bb2d829423304b465fa26edea9))
* **usp:** document Bundle 1 handlers + objects[].uniqueKeys ([1e0902e](https://github.com/herder-labs/cpe-labs/commit/1e0902e5326cbdf7eece2ce232ddd63de5d50cde))
* **usp:** runtime reconciliation subsection for Subscription table ([b13ae5e](https://github.com/herder-labs/cpe-labs/commit/b13ae5e0d7a197d55ec4c9dc0052520105c9a8ce))
* **usp:** v0 quickstart + profile-yaml reference + example profile block ([5e2a8f5](https://github.com/herder-labs/cpe-labs/commit/5e2a8f569571333a27765c66aeae2c6480104695))


### CI

* **release:** allow workflow_dispatch for releases created with GITHUB_TOKEN [skip ci] ([a96702e](https://github.com/herder-labs/cpe-labs/commit/a96702e77c622e657bb5bdfc9a432a7e1134b049))
* **release:** trigger on tag push, drop noise tags, support prereleases [skip ci] ([f028901](https://github.com/herder-labs/cpe-labs/commit/f028901a3d17a58e50b23618e93c20759bc5134b))
* **release:** un-draft existing releases on workflow run [skip ci] ([e945dd3](https://github.com/herder-labs/cpe-labs/commit/e945dd3021f4f0e5471ede559e000a1220da65df))
