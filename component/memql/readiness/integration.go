package readiness

import (
	"encoding/json"
	"errors"
)

// integrationReport is the slice of integration.<name>.status's payload the
// evaluator reads. The report's own fields, nothing invented.
type integrationReport struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Settings []struct {
		Source string `json:"source"`
	} `json:"settings"`
	Credentials []struct {
		Present bool   `json:"present"`
		Source  string `json:"source"`
	} `json:"credentials"`
}

// IntegrationStatus reads the named self-report from integration status's
// registry envelope. It returns only state and configuration presence; raw
// settings and credential values never leave this boundary, including errors.
func IntegrationStatus(payload []byte, name string) (state string, touched bool, err error) {
	var envelope struct {
		Integrations []integrationReport `json:"integrations"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return "", false, errors.New("readiness: malformed integration status envelope")
	}
	for _, rep := range envelope.Integrations {
		if rep.Name != name {
			continue
		}
		switch rep.State {
		case "configured", "unhealthy", "needs_configuration":
		default:
			return "", false, errors.New("readiness: integration status has no recognized state")
		}
		for _, s := range rep.Settings {
			if s.Source != "" && s.Source != "unset" {
				touched = true
			}
		}
		for _, c := range rep.Credentials {
			if c.Present {
				touched = true
			}
		}
		return rep.State, touched, nil
	}
	return "", false, errors.New("readiness: integration status has no matching report")
}
