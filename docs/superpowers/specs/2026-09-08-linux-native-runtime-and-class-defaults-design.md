# The native Linux runtime, and the defaults a machine class pulls -- Design

- **Date:** 2026-09-08
- **Status:** approved by the owner's direction in the 2026-09-08 session, the
  afternoon after the cockpit install wizard (epic memql#5217) shipped. The owner
  ran `memql worker setup --inference` on a Pop!_OS 24.04 machine with an RTX
  4090 and got a refusal naming three root commands; a colleague called the
  recommended set for that machine "too old and too weak" for the hardware. The
  direction: the command installs end to end, fully local, with the best models
  the hardware can hold, and the selections are checked against the routing
  policies. D1-D9 are rulings recorded for the owner to overturn.
- **Owner areas:** the cockpit's `internal/worker/inference` (`stage.go`, the
  Linux plan, the command), its Linux uninstaller and `lib.sh`; the engine's
  `dsl/models/seeds.memql`, `component/memql/fleet_recommend.go`'s contract, the
  cockpit's `inference/recommend.go` mirror, `docs/public/operate/local-models.md`.
- **Overturns:** the LINUX half of D1 in
  `2026-09-06-inference-machine-setup-design.md` (Docker as the Linux runtime);
  the class-24 row of D2 in `2026-09-07-open-weight-defaults-and-catalog-design.md`
  (24 GB pulls the 16 GB set). Both records stay as written; this one says why.

---

## 1. Problem

Two problems arrived in one terminal transcript.

**The runtime.** On Linux the cockpit's only runtime was the `ollama/ollama`
container with the GPU passed through. On every fresh machine that stops at
the same line: Docker cannot see an NVIDIA GPU without the NVIDIA container
toolkit, which is a root install; on Pop!_OS 24.04 the package is in no
configured repository at all, so the install is "add NVIDIA's apt repository
and key, install, configure the runtime, restart Docker"; and the Docker
restart restarts every container on the machine, which on a developer's box
is the k3d cluster the cockpit was about to pair with. The cockpit runs no
sudo (D2 of the 2026-09-06 record, and `install.go`'s guard), so it printed
the three commands, labelled them as the person's, and stopped. That was the
design working as written, and the design was wrong about the cost: it priced
Docker as "already on the allow-list" and never priced the toolkit.

**The models.** The class ladder is 16 / 24 / 32 / 64 / 128 GB of usable memory
and the cockpit's recommended set had no 24 rung: a 24 GB card got the 16 GB
pair, `qwen3.5:9b` and `qwen3-embedding:0.6b`, leaving two thirds of the most
common enthusiast GPU idle. The engine's catalog did have a 24 GB entry
(`gemma4:12b`), so the two copies of "what this machine should pull" -- which
the previous record said must not drift -- already disagreed. And the curated
set was verified against the Ollama library on 2026-09-07; by 2026-09-08 the
newest Qwen dense model on the library was three weeks old and the newest
Gemma mixture two, so "verified yesterday" and "current" were not the same
claim.

## 2. What was established before deciding

Baseline facts from the owner's session and vendor release on 2026-09-08, before these changes:

- `apt-cache policy nvidia-container-toolkit` answers nothing on Pop!_OS 24.04:
  the package is not in any configured repository.
- `/dev/nvidia0`, `/dev/nvidiactl` and `/dev/nvidia-uvm` are `crw-rw-rw-`, and
  `nvidia-smi -L` answers as the user. The GPU is reachable from user space
  with no further grant; that is the fact the hardware floor was already
  passing on.
- Docker 29.7.2 lists `runc` and `io.containerd.runc.v2` as its runtimes and no
  `nvidia`; the k3d cluster runs on that daemon.
- Ollama's Linux release (v0.33.3) ships as `ollama-linux-<arch>.tar.zst` --
  `bin/ollama` plus `lib/ollama/` with the CUDA 12 and 13 libraries, 1.43 GB
  compressed, 2.26 GB unpacked, 65 entries, 17 relative symlinks for shared
  library version names -- with an `ollama-linux-amd64-rocm.tar.zst` add-on
  and a `sha256sum.txt` listing every asset as `<hex>  ./<name>`. The `.tgz`
  names the previous record used answer 404. Ollama's own `install.sh`
  unpacks the same archive into `/usr/local` as root and writes a system
  unit; a user-space unpack is the same bytes without the root.
- Every shipped routing policy's first door is `fleet:strongest`
  (`dsl/policies/policies.memql`), for every level. `fleet:fastest` exists in
  the grammar and no shipped rule uses it. So on a machine holding two text
  models, every call -- `fast` included -- goes to the one with more
  parameters, and the smaller one is never chosen.
- `orderModels` ranks the fleet strongest-first by `params`, the TOTAL
  parameter count a machine advertises. A 35B mixture with 3B active
  outranks a 27B dense model on that scale.

The original model audit supplied candidate models and memory calculations.
The completion review rechecked the final tags against Ollama's library,
corrected both 27B variants to the same published 27.3B parameter count, and
replaced a copied test sorter with the engine's real recommendation function.
Benchmark evidence informs curation; it does not establish local throughput
or prove that a model wins every workload. The user's completion instruction
also includes the previously deferred routing issue, memql#5235.

## 3. Decisions

### D1 -- The Linux runtime is Ollama as the person's own user; Docker is `--runtime docker`

Ruling. `memql worker setup --inference` on Linux fetches the vendor's release
archive for the machine's architecture (plus the ROCm add-on on an AMD
machine) from the `ollama/ollama` GitHub release, checks it against that
release's `sha256sum.txt`, unpacks it into `~/.memql/ollama/runtime`, writes a
USER systemd unit `memql-ollama.service` that runs `bin/ollama serve` bound to
`127.0.0.1:11434` with `OLLAMA_MODELS=~/.memql/ollama/models`, and enables it.
Nothing in that needs root: no toolkit, no Docker, no daemon restart, no
package repository. The container path stays exactly as it was, behind
`--runtime docker`, and every refusal it can produce now ends by naming the
flag's way out.

Why native and not "the cockpit runs the sudo steps after consent". Both
would install end to end; only one keeps the cockpit's oldest rule, and the
rule is worth more than the symmetry. A cockpit that elevates on a person's
machine, even after a question, is a different product with a different
threat model, and the toolkit path would also have needed a per-distribution
package-repository branch (apt, dnf, zypper, pacman) that nobody here can
test on more than one of them. The user-space unpack is one code path on
every distribution, and it reaches the GPU through the device nodes
`nvidia-smi` already opened to pass the floor.

Why the archive and not `curl | sh`. Ollama's installer is a script that runs
as root and will install the NVIDIA driver if it decides to. The release
archive is data: fetched with the cockpit's own HTTP client, verified against
the release's checksum before it is unpacked, and every entry checked to land
inside the runtime directory before it is created (`ErrArchiveEscapes`). The
archive and the checksum file are fetched from ONE resolved release tag, so
they cannot come from two releases published minutes apart; when the tag
cannot be resolved, both fall back to the vendor's `latest` alias and a
mismatch refuses rather than guesses.

Cost if wrong: a machine whose administrator wanted the container gets a
user-space process instead, and reaches for `--runtime docker`. A machine
where the user cannot open the device nodes is refused with the group to join
(`render` and `video` for AMD) rather than left serving from the CPU.

### D2 -- The managed runtime and model directory are owned by the uninstaller

The runtime at `~/.memql/ollama/runtime`, the models at
`~/.memql/ollama/models` (`OLLAMA_MODELS` in the environment wins), the log at
`~/.memql/state/ollama.log`; only the unit file lives outside, at
`~/.config/systemd/user/memql-ollama.service`, because that is where systemd
looks. The models sit BESIDE the runtime rather than under it because the
runtime directory is replaced wholesale on an unpack, and a re-run must not
delete a night's worth of pulls. `uninstall-linux.sh` stops and removes the
unit with the worker's own; `--purge` removes `~/.memql/ollama` -- runtime and
models -- and without `--purge` the models stay, named in the closing block
with the flag that removes them. The uninstall line the OS shows on a
machine's page does not change. An external `OLLAMA_MODELS` directory and
Ollama's own per-user keys/configuration outside `~/.memql` are retained; the
uninstaller does not claim ownership of those files.

### D3 -- The stage is consented to with the commands, and runs before the first one

The archive fetch, its checksum, the unpack and the unit file are not command
lines, so they are printed as sentences above the commands ("This will:
download ... write ... and then run:") and covered by the same one question.
`InstallRuntime` runs the stage after consent and before the first command,
because `systemctl --user enable --now` starts a unit whose file the stage
writes; a stage that ran second would enable nothing and report success. A
stage that fails -- a checksum that does not match, a volume without room
(refused before the first byte, at three times the archive's size, with both
numbers in the sentence), an archive without `bin/ollama` -- runs no command
and leaves no `.new` directory. `--non-interactive` prints the stage and
refuses with exit 3 exactly as it refuses a command.

### D4 -- A started runtime is not an installed one until it answers

After the commands the setup polls the runtime's version route for up to
thirty seconds. An install command exiting zero says a service was asked to
start; the very next step pulls against that socket, and a pull against a
runtime still discovering its GPU fails with a connection error that names
nothing. A runtime silent past thirty seconds is exit 5, and the sentence
names the log (`~/.memql/state/ollama.log`).

### D5 -- Class 24 is its own rung, and the two copies of the set agree

Filled from the audit; see section 4 for the table. The cockpit's
`RecommendedSet` gains the `24` case the engine's rows already implied, and
the test that pins the two copies together pins every class the ladder
names, not the three the previous record spelled out.

### D6 -- A class's picks fit resident together, at a working context

The models a class recommends are chosen so that ALL of them stay loaded at
once inside the class's usable memory less a tenth, at the working contexts
in section 4, using
the KV-cache figures the audit computed per model (hybrid-attention and
sliding-window models keep far smaller caches than their parameter count
suggests). Ollama evicts and reloads when a set does not fit, and a
seventeen-gigabyte reload every time a call's level flips is seconds of
latency that read as the platform being slow. One model may serve more than
one level; the embedder is always its own.

### D7 -- Fast routing and mixture ordering ship in this completion

The owner included all open issues, including memql#5235. A shipped
`@when(level="fast")` rule selects the `fleet:fastest` policy, with a 3B
effective-parameter floor. This is an explicit quality heuristic, not a
benchmark threshold. A tiny model remains usable when pinned, but does not
become the platform fast default merely by advertising tool support.

Strongest ordering uses active parameters when a model advertises them,
otherwise total parameters. Ollama discovery derives active parameters only
when expert counts and tensor metadata support an exact calculation; unknown
layouts stay unknown. The owner can still rank models explicitly. The
recommended installation remains one text model plus the active embedder per
class; an already available smaller model can serve the fast lane.

### D8 -- The two repositories ship one coordinated change

Unchanged from the previous record and restated because it was the rule that
had already been broken: `inference/recommend.go` is the answer for a machine
nobody has paired yet, the engine's rows are the authority, and a change to
one without the other is what put a 4090 on the 16 GB set. The cockpit's
`.github/memql-pin` moves to the engine commit carrying the rows.

### D9 -- The native unit keeps the cache at eight bits, and every embedding call runs at 8K

Two runtime facts the audit established decide whether a class's set fits at
all. Ollama sizes a model's cache at load for the context it will run at, and
without a request value that is its VRAM-tier default: 4K under 23 GiB, 32K
from 24 to 47 GiB, 256K above (`server/routes.go`; the FAQ's "4096" is
stale). And `qwen3-embedding:0.6b` is full attention in every layer, so at
32K its cache is 3.8 GB in front of 639 MB of weights -- observed on the
owner's card as 5.8 GB of VRAM for the embedder alone. Two rulings follow:
the cockpit sends every embedding call with `num_ctx` 8192 (inputs are
chunks and queries, never a document; the runtime clamps a value above a
model's ceiling); and the native Linux unit sets
`OLLAMA_KV_CACHE_TYPE=q8_0` to reduce cache memory. Neither setting guarantees
that the text model and embedder stay resident together: compute buffers,
concurrency and other GPU applications also consume memory.

The 2026-09-08 live run on an RTX 4090 with Ollama 0.33.3 measured
2,416,873,308 GPU bytes for the embedder at 8K with `q8_0` KV:
603.87 MiB of weights, 476 MiB of KV cache and 1225.04 MiB of compute
buffers, plus about 205 MiB of host buffers. The previous 1.6 GB estimate
omitted compute buffers; the catalog now reserves 2.6 GB. This is a
conservative GPU estimate for that measured configuration, not a measured
total-process bound for every runtime or platform. Text estimates remain
unchanged.

Chat calls now carry a positive working-context request through the worker
protocol and across the replica hop to Ollama's `num_ctx`. A prompt admitted
against a model's advertised ceiling must not silently fall back to the
runtime's smaller default. Embeddings retain their separate 8192-token
working context. The native unit requests `q8_0` cache; Ollama automatically enables Flash
Attention on supported devices, which is required for cache quantization.

## 4. The recommended set, by class

Memory figures are residency estimates in decimal GB at the stated
contexts; the embedder includes measured compute buffers. They are not
measurements of total process memory. The class is
converted from GiB before reserving ten percent for runtime overhead; larger
contexts, concurrency and other GPU applications can exceed that budget.
The engine test verifies the actual selected set, and local inference checks
exercise the owner's 24 GB card. Smaller-than-class-16 machines have no
simultaneous-residency promise.

The local run returned `MEMQL_LOCAL_OK` from the 27B model at 32768 context
and a 1024-dimensional embedding at 8192 context. Another application held
about 6.5 GiB of VRAM, so Ollama evicted chat before loading the embedder.
This validates both calls separately and records a contention limitation;
it does not establish simultaneous residency. The updated 22.7 GB pair
estimate still fits the class-24 curation budget of 23.2 GB when competing
allocations are absent.

| Class | Budget | Fast / strong / reasoning | Text estimate | Embeddings | Set estimate |
|---|---|---|---|---|---|
| Below 16, above the setup floor | Varies | `qwen3.5:4b` | Context-dependent | `qwen3-embedding:0.6b` at 8K | Varies |
| 16 | 15.5 GB | `qwen3.5:9b` | 8.8 GB at 32K | 2.6 GB at 8K | 11.4 GB |
| 24 | 23.2 GB | `qwen3.8:27b` | 20.1 GB at 32K | 2.6 GB at 8K | 22.7 GB |
| 32 | 30.9 GB | `qwen3.8:27b` | 20.1 GB at 32K | 2.6 GB at 8K | 22.7 GB |
| 64 | 61.8 GB | `qwen3.8:27b-q8_0` | 38.7 GB at 256K | 2.6 GB at 8K | 41.3 GB |
| 128 | 123.7 GB | `qwen3.8:27b-q8_0` | 38.7 GB at 256K | 2.6 GB at 8K | 41.3 GB |

Evidence checked on 2026-09-08:

- Ollama's [4B tag](https://ollama.com/library/qwen3.5:4b) reports 4.66B
  parameters and a 3.4 GB Q4_K_M download. It replaces the original audit's
  9B fallback below class 16, where the larger pair could exceed memory.
- The [9B tag](https://ollama.com/library/qwen3.5:9b) reports 9.65B
  parameters and a 6.6 GB Q4_K_M download. It serves all three text levels
  at class 16, avoiding a second reasoning model in that memory budget.
- The [27B Q4 tag](https://ollama.com/library/qwen3.8:27b) and
  [Q8 tag](https://ollama.com/library/qwen3.8:27b-q8_0) both report 27.3B
  parameters, with 18 GB and 30 GB downloads respectively. The recommender
  uses precision as a tie-break after parameters and context. It does not
  inflate the Q8 parameter count to force the result.
- [Artificial Analysis](https://artificialanalysis.ai/models/qwen3-8-27b)
  reports intelligence index 34 at the highest reasoning setting and first
  place in its 4B-to-40B bracket. That supports investigating this dense
  model; it does not measure the local Q4 variant, establish RTX 4090
  throughput, or prove superiority over every larger model. The previous
  draft's unsourced local-throughput and universal-ranking claims are removed.
- [Ollama context settings](https://docs.ollama.com/context-length) and
  [cache configuration](https://docs.ollama.com/faq) explain the context and
  memory tradeoff. The embedder's 8K working context is a runtime setting,
  independent of the model's advertised maximum.

Larger models and mixture models stay available for manual selection. The
64/128 GB default spends more memory on the same model's precision rather
than installing another text model automatically. Operators can measure
their own workload and use preferences or explicit pins to choose differently.
The embedder remains the cluster's active binding at every class; alternatives
need a deliberate binding change before cluster traffic uses them.

## 5. What this does not change

- The macOS path (Homebrew, native) and its refusals.
- The hardware floor, the class ladder and the `unsupported` bucket.
- The pull, `models.allow`, the re-advertise, the ModelPull message pair.
- The OS wizard's install stop: the second command was already
  `memql worker setup --inference`, and the machine now finishes it.
