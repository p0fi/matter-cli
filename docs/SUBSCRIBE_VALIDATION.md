# Manual validation: foreground attribute subscriptions

`cluster subscribe` / shorthand `subscribe` are covered by automated tests at
the interaction-client exchange seam (`internal/interaction`) and the CLI
helper-function seam (`cli`, `cli/output`) — see `docs/GIT_WORKFLOW.md` for
how to run those. Neither seam exercises a real Matter transport end to end,
so protocol interoperability (a real SubscribeRequest/SubscribeResponse/
ReportData exchange over UDP with a peer) is validated manually against the
`matter-js-test-device` virtual On/Off light instead of a
`//go:build integration` hardware test, per issue #77's testing decision.

## Setup

1. Start a virtual On/Off light using the `matter-js-test-device` skill (or
   manually via `matter.js`'s example light app) and commission it:

   ```bash
   matter commission code <the printed pairing code>
   ```

2. Note the commissioned node ID (`matter fabric ls`) and use it below as
   `@N`.

## Checks

Run each of these against the virtual light, from a terminal with a second
terminal open to toggle the attribute mid-stream:

1. **Priming value + live report + clean Ctrl+C**

   ```bash
   matter @N/1 OnOff subscribe OnOff
   ```

   Expect the current `OnOff` value printed immediately (the priming
   report), then — after toggling the light from the second terminal with
   `matter @N/1 OnOff Toggle` — a second line with the new value. Press
   Ctrl+C: the command must exit `0` (`echo $?`) and stop cleanly, no error
   text.

2. **`--count` terminates after the priming value**

   ```bash
   matter @N/1 OnOff subscribe OnOff --count 1
   ```

   Expect exactly one record (the priming value), then a clean exit `0`
   without waiting for Ctrl+C.

3. **`--duration` terminates after the requested window**

   ```bash
   matter @N/1 OnOff subscribe OnOff --duration 5s
   ```

   Expect the command to exit `0` on its own after ~5 seconds.

4. **NDJSON output**

   ```bash
   matter @N/1 OnOff subscribe OnOff --count 1 --format json | tee /tmp/sub.ndjson
   jq . /tmp/sub.ndjson   # must parse without error; value must be a JSON bool
   ```

5. **Multi-document YAML output**

   ```bash
   matter @N/1 OnOff subscribe OnOff --count 1 --format yaml
   ```

   Expect a `---`-prefixed document with a native `value: true`/`value:
   false` (not a quoted string).

6. **stdout/stderr separation**

   ```bash
   matter @N/1 OnOff subscribe OnOff --count 1 --format json 2>/tmp/sub.stderr 1>/tmp/sub.stdout
   cat /tmp/sub.stdout   # exactly one JSON object, nothing else
   cat /tmp/sub.stderr   # lifecycle text only (or empty, if stderr isn't a TTY)
   ```

7. **`-K`/`--keep-alive` is ignored for subscribe, not honored and not left running**

   ```bash
   matter session stop   # make sure no daemon is running first
   matter -K 2m @N/1 OnOff subscribe OnOff --count 1
   matter session status  # must say "not running" — no daemon was spawned
   ```

   Expect a `--keep-alive is ignored for subscribe` warning on stderr, then
   the subscription to run normally to completion. No daemon process may be
   spawned for this invocation (`matter session status` afterward must still
   report "not running").

   Separately, confirm the pre-existing-daemon case still fails fast instead
   of hanging:

   ```bash
   matter -K 2m @N/1 OnOff read OnOff   # starts a real daemon
   matter @N/1 OnOff subscribe OnOff --count 1
   ```

   Expect the second command to fail immediately with "a session daemon is
   running and holds the database lock" rather than hang. Clean up with
   `matter session stop`.

Clean up with `matter session stop` and `matter decommission @N` (or `matter
fabric reset`) when done.
