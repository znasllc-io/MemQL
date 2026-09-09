package overlays

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A NodeService advertisement identifies one replica. Service DNS is only a
// bootstrap target: a load balancer cannot route to the worker stream's owner.
func TestEveryRenderedNodeAdvertisesItsPodListeningAddress(t *testing.T) {
	for _, overlay := range []string{"local", "cloud"} {
		t.Run(overlay, func(t *testing.T) {
			seen := map[string]bool{}
			dec := yaml.NewDecoder(strings.NewReader(render(t, overlay)))
			for {
				var doc struct {
					Kind     string `yaml:"kind"`
					Metadata struct {
						Name string `yaml:"name"`
					} `yaml:"metadata"`
					Spec struct {
						Template struct {
							Spec struct {
								Containers []struct {
									Name  string `yaml:"name"`
									Ports []struct {
										Name          string `yaml:"name"`
										ContainerPort int    `yaml:"containerPort"`
									} `yaml:"ports"`
									Env []struct {
										Name      string `yaml:"name"`
										Value     string `yaml:"value"`
										ValueFrom struct {
											FieldRef struct {
												FieldPath string `yaml:"fieldPath"`
											} `yaml:"fieldRef"`
										} `yaml:"valueFrom"`
									} `yaml:"env"`
								} `yaml:"containers"`
							} `yaml:"spec"`
						} `yaml:"template"`
					} `yaml:"spec"`
				}
				if err := dec.Decode(&doc); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if doc.Kind != "Deployment" && doc.Kind != "Rollout" {
					continue
				}
				for _, c := range doc.Spec.Template.Spec.Containers {
					port := 0
					for _, p := range c.Ports {
						if p.Name == "node" {
							port = p.ContainerPort
						}
					}
					if port == 0 {
						continue
					}
					seen[doc.Metadata.Name] = true
					address, listener := "", ""
					ipIndex, addressIndex := -1, -1
					for i, env := range c.Env {
						switch env.Name {
						case "POD_IP":
							if env.ValueFrom.FieldRef.FieldPath != "status.podIP" {
								t.Errorf("%s POD_IP is not the Downward API pod address", doc.Metadata.Name)
							}
							ipIndex = i
						case "MEMQL_NODE_ADDRESS":
							address, addressIndex = env.Value, i
						case "MEMQL_NODE_SERVICE_ADDRESS":
							listener = env.Value
						}
					}
					if ipIndex < 0 || addressIndex <= ipIndex {
						t.Errorf("%s must declare POD_IP before expanding its advertised address", doc.Metadata.Name)
					}
					if want := fmt.Sprintf("$(POD_IP):%d", port); address != want {
						t.Errorf("%s advertises %q; want replica address %q", doc.Metadata.Name, address, want)
					}
					if listener != fmt.Sprintf(":%d", port) {
						t.Errorf("%s advertisement port %d differs from actual listener %q", doc.Metadata.Name, port, listener)
					}
					if strings.ReplaceAll(address, "$(POD_IP)", "10.42.0.10") == strings.ReplaceAll(address, "$(POD_IP)", "10.42.0.11") {
						t.Errorf("%s replicas advertise the same address", doc.Metadata.Name)
					}
				}
			}
			for _, name := range meshDeployments {
				if !seen[name] {
					t.Errorf("missing node workload %s", name)
				}
			}
		})
	}
}
