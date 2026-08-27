# Obscura no-render evaluation

Obscura 0.2.1 no-render can host Rush's controller and execute browser bundles,
but it does not preserve Rush's real-browser behavior. The adapter remains an
opt-in prototype, and no Kodē performance claim is valid from this evaluation.

## Adapter and method

The Linux `rush_obscura` build starts one isolated `obscura serve` process for
each Rush browser realm, creates and attaches one page target, installs Rush's
Go bindings through `Runtime.addBinding`, and navigates to the existing loopback
controller. Obscura does not continuously advance all page tasks after the
initiating CDP evaluation returns, so the adapter sends a bounded no-op
evaluation every 10 milliseconds while a batch is active to keep timers and
queued browser work moving.

The evaluated binary was the official x86-64 Linux no-render archive for
Obscura 0.2.1, with SHA-256
`bf60fff504f15bf6e16b22cbbeefe99348f247d7f95d6bde5d06b34a7d9d9d9c`.
The Kodē input was the unsharded browser partition on its Rush migration branch:
125 files and an expected 1,040 tests. `RUSH_WEBVIEW_POOL_SIZE=2` selected two
Rush realms, and process observation confirmed exactly two simultaneous
`obscura serve` engine processes.

## Results

The complete Kodē partition did not produce a valid result. It remained active
for more than eight minutes before the evaluation session interrupted it, well
beyond the existing two-worker Vitest complete-command median of 34.810 seconds.
Because the partition did not pass and complete, the conditional five fresh-run
timings were not recorded and no median comparison was calculated.

A deterministic 10-file slice completed and made the incompatibility concrete:

| Input | Result | Complete command | Engine processes |
| --- | ---: | ---: | ---: |
| First 10 Kodē browser files | 102 pass, 4 fail (106 total) | 5,613.388 ms | 2 |

Rush's four-file browser self-test also completed with 30 passes and 3 failures.
The failures expose behavior required by the existing assertions:

- Testing Library reports ordinary text inputs associated with labels as
  non-labellable, so Kodē's accessible form queries fail.
- Checking a checkbox through Rush's normal DOM interaction does not set its
  checked state.
- Computed style does not reflect injected suite CSS, and color/style matching
  diverges from browser normalization.

These are runtime differences, not Kodē test defects. Skipping the affected
tests, replacing accessible queries, or weakening style and interaction
assertions would violate the migration boundary. Obscura no-render is therefore
not a valid runtime for the current Kodē browser partition.
