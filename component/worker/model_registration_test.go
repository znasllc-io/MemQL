package worker

import (
	"reflect"
	"testing"

	memqlv1 "github.com/znasllc-io/memql/component/grpc/gen"
)

// A model can pass routing tests and still never become available if the
// registration gate refuses MODEL. Run the real validator and persistence
// preparation with the capability, descriptor and concurrency Cockpit sends.
func TestModelRegistrationCreateAndReconnect(t *testing.T) {
	for _, reconnect := range []bool{false, true} {
		name := "create"
		if reconnect {
			name = "reconnect"
		}
		t.Run(name, func(t *testing.T) {
			store := &fakeRegistrationStore{}
			if reconnect {
				store.existing = existingRow(nil)
			}
			register := &memqlv1.Register{
				Name:                     "inference-worker",
				Capabilities:             []string{CapabilityHeadless, CapabilityComputerUse, ModelCapability},
				CapabilityDescriptorJson: validDescriptorJSON,
				Labels:                   map[string]string{"model:qwen3.5:9b": "true"},
				Concurrency:              map[string]uint32{CapabilityHeadless: 8, CapabilityComputerUse: 1, ModelCapability: 2},
			}
			row := runUpsert(t, store, register)
			var persisted RegistrationRow
			if reconnect {
				if len(store.refreshed) != 1 || len(store.created) != 0 {
					t.Fatalf("reconnect created %d / refreshed %d rows", len(store.created), len(store.refreshed))
				}
				persisted = store.refreshed[0]
			} else {
				if len(store.created) != 1 || len(store.refreshed) != 0 {
					t.Fatalf("initial registration created %d / refreshed %d rows", len(store.created), len(store.refreshed))
				}
				persisted = store.created[0]
			}
			for _, got := range []RegistrationRow{row, persisted} {
				if !reflect.DeepEqual(got.Capabilities, register.Capabilities) {
					t.Errorf("capabilities = %v, want %v", got.Capabilities, register.Capabilities)
				}
				if !reflect.DeepEqual(got.Concurrency, register.Concurrency) {
					t.Errorf("concurrency = %v, want %v", got.Concurrency, register.Concurrency)
				}
				if got.Labels["model:qwen3.5:9b"] != "true" {
					t.Errorf("model label lost: %v", got.Labels)
				}
				if !reflect.DeepEqual(got.CapabilityDescriptor, mustDescriptor(t, validDescriptorJSON)) {
					t.Errorf("capability descriptor lost or changed: %+v", got.CapabilityDescriptor)
				}
			}
		})
	}
}
