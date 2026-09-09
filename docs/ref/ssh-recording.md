# SSH session recording

Session recording keeps the terminal of every SSH session that Tailscale
SSH admits, the way Tailscale's [session
recording](https://tailscale.com/docs/features/tailscale-ssh-session-recording)
does. The client on the machine being logged into streams the session to
a recorder node as it happens, and the recorder writes it to an
[asciinema](https://asciinema.org) file. Slopscale can run the recorder
inside the server, or the policy can name any other recorder, such as
Tailscale's `tsrecorder`.

## The embedded recorder

Turn it on in the config file:

```yaml
ssh_recording:
  enabled: true
  dir: /var/lib/slopscale/recordings
  state_dir: /var/lib/slopscale/recorder
  retention: 2160h
```

The server then joins its own tailnet as a node named `slopscale-recorder`
with the tag `tag:slopscale-recorder`, approved and reachable on port 80
from every machine, and stores each session under `dir` as one `.cast`
file. `state_dir` holds the node's keys, so it keeps its identity across
restarts; on the first start the server mints a short-lived pre-auth key
for it. `retention` deletes recordings older than the duration, hourly;
leave it unset to keep them.

The embedded recorder is a default recorder for every SSH rule. It needs
no entry in the policy, and the policy needs no rule for it: the server
adds a grant that lets every machine reach every recorder on its port.

## Other recorders

The tailnet default is a setting: `slopscale settings set --ssh-recorders tag:recorder`, `POST /api/v1/settings` with `sshRecorders`, or the _SSH
session recording_ section of the console's _Settings_ page. A recorder is
a tag, a host from the policy's `hosts` section, or a tailnet address. The
default applies to every SSH rule that names no recorder of its own.

A rule names its own with `recorder`, which wins over the default, and can
require it with `enforceRecorder`:

```json
{
  "ssh": [
    {
      "action": "accept",
      "src": ["group:ops"],
      "dst": ["tag:prod"],
      "users": ["autogroup:nonroot"],
      "recorder": ["tag:recorder"],
      "enforceRecorder": true
    }
  ]
}
```

A `check` rule records like an `accept` rule once the check passes.

## Enforcement

Without enforcement, a session whose recorder cannot be reached goes on
unrecorded, and the client reports the failure to the server. With it, the
client rejects the session when no recorder answers, and ends a session
whose recording breaks off. The tailnet-wide switch is `slopscale settings set --ssh-recording-enforce`, `sshRecordingEnforce` in the API, or _Require
recording_ in the console; it covers the default recorders. A rule's
`enforceRecorder` covers the rule's own recorders, or the default ones when
the rule names none.

Every failure the client reports lands in the [audit log](audit.md) as
`ssh.recording.rejected`, `ssh.recording.terminated` or
`ssh.recording.failed` on the machine that was logged into, with the
connecting machine, the SSH user and the recorders tried, and fires the
`sshRecordingFailed` [webhook](webhooks.md) event.

## Recordings

Recordings from the embedded recorder are listed newest first on the
console's _SSH sessions_ page, by `slopscale ssh-recordings list`, or
through `/api/v1/ssh-recording`. Each carries who connected from where,
the SSH user and local account, the command when one ran instead of a
shell, the file's size and whether the upload ended cleanly. A recording
still being uploaded is listed as such.

```console
$ slopscale ssh-recordings list
$ slopscale ssh-recordings download -i 12
$ asciinema play ssh-recording-12.cast
```

`download` writes the asciinema file, `-o -` sends it to standard output,
and `delete` removes the recording and its file. Listing and downloading
need the `logs:configuration:read` scope, which every admin role and the
auditor hold; deleting needs `logs:configuration`.

Recordings sent to another recorder live wherever that recorder keeps
them; Slopscale only knows about its own.

## How it works

The server puts the recorders' addresses on the SSH action it sends the
machine being logged into, with `/machine/ssh/event` as the address to
report failures to. When a session starts, that machine's client posts the
session to the first recorder that answers. The embedded recorder speaks
the same upload protocol as `tsrecorder`, one HTTP POST per session whose
body is the asciinema stream, so a client that works with Tailscale's
recorder works with it unchanged. Recordings are indexed in the
`ssh_recordings` table, and the recorder's node is an ordinary tagged node
in every other respect.
