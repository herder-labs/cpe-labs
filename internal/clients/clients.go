// Package clients drives row-count churn on multi-instance object tables
// to simulate WiFi-station / LAN-host populations rotating over time.
//
// A Fabricator is one (table, target-count, churn-cadence) combination.
// Tick adjusts the table's row count toward TargetCount by ChurnRate
// rows per call (add or drop). Adds use Tree.AddObject then
// SetBatch-overlay RowDefaults with {seq} expanded to the new
// instance number; drops use Tree.DeleteObject on the highest-numbered
// instance.
//
// All writes flow through the existing Tree.OnWrite chain so the
// Subscription evaluator picks up autonomous Notify(ObjectCreation /
// Deletion) emission when a matching Subscription exists.
//
// Fleet placeholders ({cpe}, {cpe:MAC:N}, etc.) in RowDefaults are
// pre-resolved by the caller before constructing the Fabricator; the
// package only expands {seq} forms at Tick time.
package clients
