// Tests for scripts/install/seed-bootstrap.sh (capability
// install.seedBootstrap, znasllc-io/memql#3375).
//
// THE assertion this file exists for: AN INCOMPLETE BOOTSTRAP SET IS EXIT 2.
//
// Identity auto-bootstraps only when ALL five of domain / owner-email /
// owner-first-name / owner-last-name / registration-mode are present
// (component/identity/config.go: BootstrapConfig.HasAllRequired). Miss one and
// the whole set is inert -- and nothing anywhere says so. The Secret is written,
// kubectl shows four healthy keys, the cluster comes up green, and the operator
// lands on a login page for an account that was never created. A partial seed
// looks MORE finished than no seed at all, which is exactly why it must not be
// a warning: a warning scrolls past in a hundred lines of cluster bring-up and
// the failure surfaces twenty minutes later as "I can't sign in".
//
// THE SECOND ASSERTION USED TO BE "the API key never appears in argv", and it
// is gone with the key (epic memql#5088): both AI vendors are reached by
// workload identity federation, so this step seeds no credential at all. What
// stands in its place is the assertion that the key path CANNOT COME BACK --
// --provider and --provider-key-file are undeclared, so cap_parse_flags
// refuses them rather than ignoring them, and a caller who believes a key was
// seeded is told otherwise instead of finding out at the first model call.
//
// Hermetic: kubectl is a stub on a PATH prefix that records argv and answers
// cluster-info / get namespace; nothing touches a real cluster.
package install

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type sbEnvelope struct {
	OK         bool            `json:"ok"`
	Capability string          `json:"capability"`
	Changed    bool            `json:"changed"`
	Result     json.RawMessage `json:"result"`
	Error      *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type sbResult struct {
	Namespace         string `json:"namespace"`
	Secret            string `json:"secret"`
	Domain            string `json:"domain"`
	OwnerEmail        string `json:"ownerEmail"`
	RegistrationMode  string `json:"registrationMode"`
	BootstrapComplete bool   `json:"bootstrapComplete"`
	KeyCount          int    `json:"keyCount"`
	DryRun            bool   `json:"dryRun"`
}

func sbScript(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	p := filepath.Join(filepath.Dir(thisFile), "seed-bootstrap.sh")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("seed-bootstrap.sh not found at %s: %v", p, err)
	}
	return p
}

func sbRun(t *testing.T, extraEnv []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	cmd := exec.Command("bash", append([]string{sbScript(t)}, args...)...)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stdin = nil
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run: %v", err)
		}
	}
	return out.String(), errb.String(), code
}

func sbParse(t *testing.T, stdout string) (sbEnvelope, sbResult) {
	t.Helper()
	line := strings.TrimSpace(stdout)
	if line == "" {
		t.Fatal("no envelope on stdout")
	}
	if strings.Contains(line, "\n") {
		t.Fatalf("stdout carried more than one line -- human logs belong on stderr:\n%s", line)
	}
	var env sbEnvelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		t.Fatalf("stdout is not a JSON envelope: %v\n%s", err, line)
	}
	if env.Capability != "install.seedBootstrap" {
		t.Errorf("capability = %q, want install.seedBootstrap", env.Capability)
	}
	var res sbResult
	if err := json.Unmarshal(env.Result, &res); err != nil {
		t.Fatalf("result is not the expected object: %v\n%s", err, env.Result)
	}
	return env, res
}

// -----------------------------------------------------------------------
// A cluster made entirely of a stub
// -----------------------------------------------------------------------

type sbWorld struct {
	env     []string
	argvLog string
}

// sbNewWorld puts a stub kubectl on a PATH prefix. It appends its full argv to
// $STUB_ARGV, answers cluster-info / get namespace successfully, and echoes
// stdin back on `apply` so the pipeline behaves like the real thing.
func sbNewWorld(t *testing.T) sbWorld {
	t.Helper()
	bin := t.TempDir()
	argvLog := filepath.Join(t.TempDir(), "kubectl.argv")

	stub := `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STUB_ARGV"
# Drop leading --context <name> so the verb matching below is uniform.
if [[ "${1:-}" == "--context" ]]; then shift 2; fi
case "${1:-}" in
  cluster-info) echo "Kubernetes control plane is running"; exit 0 ;;
  get)          exit 0 ;;
  create)       echo "apiVersion: v1"; echo "kind: Secret"; exit 0 ;;
  apply)        cat > /dev/null; echo "secret/memql-bootstrap configured"; exit 0 ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write kubectl stub: %v", err)
	}
	return sbWorld{
		env: []string{
			"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
			"STUB_ARGV=" + argvLog,
		},
		argvLog: argvLog,
	}
}

func (w sbWorld) argv(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(w.argvLog)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read argv log: %v", err)
	}
	return string(b)
}

// sbCompleteArgs is a fully-formed invocation, so a negative test proves the
// refusal it names rather than a different missing param.
func sbCompleteArgs() []string {
	return []string{
		"--domain=memql.localhost",
		"--owner-email=ada@example.com",
		"--owner-first-name=Ada",
		"--owner-last-name=Lovelace",
		"--registration-mode=invite_only",
	}
}

// -----------------------------------------------------------------------
// THE assertion: an incomplete set is exit 2, not a warning
// -----------------------------------------------------------------------

func TestSeedBootstrapIncompleteSetIsExitTwo(t *testing.T) {
	required := []string{
		"--domain=memql.localhost",
		"--owner-email=ada@example.com",
		"--owner-first-name=Ada",
		"--owner-last-name=Lovelace",
		"--registration-mode=invite_only",
	}
	for i, omitted := range required {
		name := strings.SplitN(strings.TrimPrefix(omitted, "--"), "=", 2)[0]
		t.Run("missing "+name, func(t *testing.T) {
			w := sbNewWorld(t)
			var args []string
			for j, a := range required {
				if j != i {
					args = append(args, a)
				}
			}
			stdout, _, code := sbRun(t, w.env, args...)
			if code != 2 {
				t.Fatalf("omitting --%s exited %d, want 2 (bad param)\nstdout: %s", name, code, stdout)
			}
			env, _ := sbParse(t, stdout)
			if env.OK || env.Error == nil || env.Error.Code != 2 {
				t.Errorf("want ok=false error.code=2, got: %s", stdout)
			}
			if !strings.Contains(env.Error.Message, name) {
				t.Errorf("the refusal must name the missing field %q; got %q", name, env.Error.Message)
			}
			if env.Changed {
				t.Error("changed=true on a refusal")
			}
			// Nothing may have been written, and the cluster must not even
			// have been asked a question.
			if got := w.argv(t); got != "" {
				t.Errorf("kubectl was invoked despite the incomplete set:\n%s", got)
			}
		})
	}
}

// One re-run per missing field is the shape of a bad error message. Naming all
// of them at once is the difference between one fix and four.
func TestSeedBootstrapNamesEveryMissingFieldAtOnce(t *testing.T) {
	w := sbNewWorld(t)
	stdout, _, code := sbRun(t, w.env, "--domain=memql.localhost")
	if code != 2 {
		t.Fatalf("exit %d, want 2\nstdout: %s", code, stdout)
	}
	env, _ := sbParse(t, stdout)
	for _, want := range []string{"owner-email", "owner-first-name", "owner-last-name", "registration-mode"} {
		if !strings.Contains(env.Error.Message, want) {
			t.Errorf("the refusal omits %q -- an operator would re-run once per field; got %q", want, env.Error.Message)
		}
	}
}

// -----------------------------------------------------------------------
// THE second assertion: no credential can be seeded here at all
// -----------------------------------------------------------------------

// TestSeedBootstrapSeedsNoVendorCredential is the successor to
// TestSeedBootstrapKeyNeverAppearsInArgv, and it asserts the stronger thing:
// not "the key is handled safely" but "there is no key".
//
// Refusal rather than silence is the point. A caller that still passes
// --provider-key-file gets exit 2 naming the flag, so an installer cannot
// report a seeded credential having seeded nothing -- which is how a cluster
// comes up green and fails at the first model call.
func TestSeedBootstrapSeedsNoVendorCredential(t *testing.T) {
	for _, flag := range []string{"--provider=anthropic", "--provider-key-file=/tmp/nope.key"} {
		t.Run(flag, func(t *testing.T) {
			w := sbNewWorld(t)
			stdout, _, code := sbRun(t, w.env, append(sbCompleteArgs(), flag)...)
			if code != 2 {
				t.Fatalf("%s exited %d, want 2 (bad param)\nstdout: %s", flag, code, stdout)
			}
			env, _ := sbParse(t, stdout)
			if env.OK || env.Error == nil || env.Error.Code != 2 {
				t.Errorf("want ok=false error.code=2, got: %s", stdout)
			}
		})
	}

	// And a clean run writes no vendor key into the Secret: no --from-file at
	// all, and nothing whose name ends in API_KEY.
	w := sbNewWorld(t)
	stdout, _, code := sbRun(t, w.env, sbCompleteArgs()...)
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstdout: %s", code, stdout)
	}
	argv := w.argv(t)
	if strings.Contains(argv, "--from-file=") {
		t.Errorf("the seed still stages a file into the Secret; nothing here has file-shaped content any more:\n%s", argv)
	}
	if strings.Contains(argv, "API_KEY") {
		t.Errorf("a vendor API key name reached the Secret:\n%s", argv)
	}
}

// -----------------------------------------------------------------------
// what actually lands in the Secret
// -----------------------------------------------------------------------

func TestSeedBootstrapWritesTheBootstrapEnvNames(t *testing.T) {
	w := sbNewWorld(t)
	args := append(sbCompleteArgs(), "--org-name=Acme", "--internal-domains=acme.com", "--internal-default-role=admin")
	stdout, stderr, code := sbRun(t, w.env, args...)
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	env, res := sbParse(t, stdout)
	if !env.Changed {
		t.Error("changed=false after writing the Secret")
	}
	if !res.BootstrapComplete {
		t.Error("bootstrapComplete=false on a complete set")
	}

	argv := w.argv(t)
	for _, want := range []string{
		"MEMQL_IDENTITY_BOOTSTRAP_DOMAIN=memql.localhost",
		"MEMQL_IDENTITY_BOOTSTRAP_OWNER_EMAIL=ada@example.com",
		"MEMQL_IDENTITY_BOOTSTRAP_OWNER_FIRST_NAME=Ada",
		"MEMQL_IDENTITY_BOOTSTRAP_OWNER_LAST_NAME=Lovelace",
		"MEMQL_IDENTITY_BOOTSTRAP_REGISTRATION_MODE=invite_only",
		"MEMQL_IDENTITY_BOOTSTRAP_ORG_NAME=Acme",
		"MEMQL_IDENTITY_BOOTSTRAP_INTERNAL_DOMAINS=acme.com",
		"MEMQL_IDENTITY_BOOTSTRAP_INTERNAL_DEFAULT_ROLE=admin",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("the Secret was written without %s:\n%s", want, argv)
		}
	}
}

// An unset optional must be ABSENT, not empty: an empty env var reads as
// "configured, to nothing" and silently overrides whatever default the engine
// would have applied.
func TestSeedBootstrapOmitsUnsetOptionals(t *testing.T) {
	w := sbNewWorld(t)
	stdout, _, code := sbRun(t, w.env, sbCompleteArgs()...)
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstdout: %s", code, stdout)
	}
	argv := w.argv(t)
	for _, absent := range []string{
		"MEMQL_IDENTITY_BOOTSTRAP_ORG_NAME=",
		"MEMQL_IDENTITY_BOOTSTRAP_NOTIFY_EMAILS=",
		"MEMQL_IDENTITY_BOOTSTRAP_INTERNAL_DOMAINS=",
	} {
		if strings.Contains(argv, absent) {
			t.Errorf("unset optional %s was written as an empty value:\n%s", absent, argv)
		}
	}
}

// -----------------------------------------------------------------------
// the modes the engine itself refuses
// -----------------------------------------------------------------------

func TestSeedBootstrapRejectsBadModes(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{
			// identity's Config.Validate refuses this at boot; catching it here
			// turns a crash-looping node into one clear sentence.
			name: "domain_restricted with no allowlist",
			args: []string{
				"--domain=memql.localhost", "--owner-email=a@b.c",
				"--owner-first-name=A", "--owner-last-name=B",
				"--registration-mode=domain_restricted",
			},
		},
		{
			name: "unknown registration mode",
			args: []string{
				"--domain=memql.localhost", "--owner-email=a@b.c",
				"--owner-first-name=A", "--owner-last-name=B",
				"--registration-mode=freeforall",
			},
		},
		{
			name: "unknown internal default role",
			args: append(sbCompleteArgs(), "--internal-default-role=superuser"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := sbNewWorld(t)
			stdout, _, code := sbRun(t, w.env, tc.args...)
			if code != 2 {
				t.Fatalf("exit %d, want 2 (bad param)\nstdout: %s", code, stdout)
			}
			env, _ := sbParse(t, stdout)
			if env.OK || env.Error == nil || env.Error.Code != 2 {
				t.Errorf("want ok=false error.code=2, got: %s", stdout)
			}
		})
	}
}

// -----------------------------------------------------------------------
// prerequisites + dry run
// -----------------------------------------------------------------------

func TestSeedBootstrapMissingKubectlIsPrerequisite(t *testing.T) {
	// A PATH carrying the shell utilities the capability library itself needs,
	// and nothing else -- so kubectl is genuinely absent rather than the whole
	// script failing to run.
	dir := raSanitizedBin(t)
	stdout, _, code := sbRun(t, []string{"PATH=" + dir, "STUB_ARGV=" + filepath.Join(t.TempDir(), "argv")},
		sbCompleteArgs()...)
	if code != 4 {
		t.Fatalf("exit %d, want 4 (prerequisite missing)\nstdout: %s", code, stdout)
	}
	env, _ := sbParse(t, stdout)
	if env.OK || env.Error == nil || env.Error.Code != 4 {
		t.Errorf("want ok=false error.code=4, got: %s", stdout)
	}
}

// A dry run must write nothing and say so -- including not asking the cluster,
// which may not exist yet when an operator is checking their inputs.
func TestSeedBootstrapDryRunWritesNothing(t *testing.T) {
	w := sbNewWorld(t)
	stdout, _, code := sbRun(t, w.env, append(sbCompleteArgs(), "--dry-run")...)
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstdout: %s", code, stdout)
	}
	env, res := sbParse(t, stdout)
	if env.Changed {
		t.Error("changed=true on a dry run")
	}
	if !res.DryRun {
		t.Error("result.dryRun=false on a dry run")
	}
	if got := w.argv(t); got != "" {
		t.Errorf("a dry run invoked kubectl:\n%s", got)
	}
}

func TestSeedBootstrapIsIdempotent(t *testing.T) {
	w := sbNewWorld(t)
	for i := 0; i < 2; i++ {
		stdout, _, code := sbRun(t, w.env, sbCompleteArgs()...)
		if code != 0 {
			t.Fatalf("run %d exited %d, want 0\nstdout: %s", i, code, stdout)
		}
	}
	// Both runs go through create|apply -- the idiom every seeder here uses.
	if n := strings.Count(w.argv(t), "apply -f -"); n != 2 {
		t.Errorf("expected two applies across two runs, got %d:\n%s", n, w.argv(t))
	}
}

func TestSeedBootstrapPrintSpec(t *testing.T) {
	stdout, _, code := sbRun(t, nil, "--print-spec")
	if code != 0 {
		t.Fatalf("--print-spec exited %d\n%s", code, stdout)
	}
	var spec struct {
		Capability string `json:"capability"`
		Params     []struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &spec); err != nil {
		t.Fatalf("spec is not JSON: %v\n%s", err, stdout)
	}
	if spec.Capability != "install.seedBootstrap" {
		t.Errorf("capability = %q, want install.seedBootstrap", spec.Capability)
	}
	names := map[string]bool{}
	for _, p := range spec.Params {
		names[p.Name] = true
	}
	for _, want := range []string{
		"domain", "owner-email", "owner-first-name", "owner-last-name",
		"registration-mode",
	} {
		if !names[want] {
			t.Errorf("--print-spec omits the %q param", want)
		}
	}
	// The retired key surface, absent by declaration: cap_parse_flags refuses
	// an undeclared flag, so leaving these out of the spec is what makes a key
	// impossible to pass rather than merely pointless (epic memql#5088).
	for _, gone := range []string{"provider", "provider-key-file"} {
		if names[gone] {
			t.Errorf("--print-spec still declares %q; there is no vendor API key to seed", gone)
		}
	}
}
