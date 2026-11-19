# Wake-hold review evidence for PR #669

This branch contains reproducer tests, not a production fix or a proposed PR.
It is based on PR #669 at `83f58e77697a8ba618bf6acd5113c99353fe88c2`.
It does not include #668 or the experimental target-keyed wake-signal change.

The new tests are opt-in with the `wakehold_evidence` build tag, so ordinary test
commands continue to run the existing suite. The helper changes only allow a
hostname and global idle delay to be selected; existing callers retain defaults.
No provider infrastructure, dependencies, or production source changes are added.

## Run the reproductions

Use Go 1.27.4, or enable automatic toolchain selection:

```sh
GOTOOLCHAIN=auto go test -race -count=3 -tags=wakehold_evidence -v ./server -run '^TestWakeHoldEvidence'
```

Expected exit: **1**. The failures expose the two reported behaviors and are not
an unexpected failure of the evidence branch. Two controls should pass:

| Scenario | Observed result |
| --- | --- |
| Two held clients using the same hostname | PASS |
| Two held clients using different hostnames for one target | FAIL: the second receives the configured kick packet |
| 2s global idle delay; 500ms plugin response delay | PASS |
| 300ms global idle delay; 500ms plugin response delay | FAIL: sleeper invoked before response/admission |

Ordinary regression checks, without enabling those desired-behavior assertions:

```sh
GOTOOLCHAIN=auto go test -count=1 ./...
GOTOOLCHAIN=auto go test -race -count=1 ./...
```

To run just one opt-in scenario:

```sh
GOTOOLCHAIN=auto go test -race -count=1 -tags=wakehold_evidence -v ./server -run '^TestWakeHoldEvidenceAliases$/different_hostname_same_target$'
GOTOOLCHAIN=auto go test -race -count=1 -tags=wakehold_evidence -v ./server -run '^TestWakeHoldEvidenceHandoff$/300ms_global_delay$'
```

## Player scenarios and observations

### Different hostnames, one resource

Think of `play.example.com` and `survival.example.com` referring to the same
scalable server. One player starts a wake and is held. A second player joins
through the other hostname while that wake is in progress. The second receives
Login Disconnect with the configured wake-kick text, rather than joining the
hold. The same-hostname control confirms that two held clients can otherwise
share the wake.

The tests wait for the first hold request before starting the second login.
They do not exercise simultaneous initial arrivals or wake-completion races.

### Wake completion before client admission

A successful wake finishes while the client's first plugin response is still
pending. With a 300ms global idle delay and 500ms response delay, the sleeper is
invoked before the client has answered or been admitted. The sleeper captures
the client's response timestamp and admitted-connection count at invocation;
both are zero. This avoids relying on the backend's delayed receipt report.

The two-second idle-delay control holds everything else the same. Timings are
intentionally compressed to expose ordering, not to suggest a normal minutes-
long timeout is affected by a 500ms response delay.

## Limits

- Synthetic protocol-758 clients, loopback TCP backend, and recording sleeper.
- No real Minecraft client or actual Docker/Swarm/Kubernetes shutdown tested.
- A sleeper invocation is observed; an actual server disconnection is not proved.
- Tests describe desired behavior for discussion, not an agreed fix requirement.
- A future shared wake coordinator must preserve each route's selected endpoint,
  including different proxies. No prototype fix is included in this branch.
- Raw execution logs and exit records are kept outside the repo. No results file
  or run-specific output should be committed.
