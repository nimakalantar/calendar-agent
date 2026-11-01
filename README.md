# calendar-agent

Reusable desired-state reconciliation and a Google Calendar client/backend for
Go applications that manage generated calendar events.

## Packages

- `src/calendaragent` — reconciliation, ownership, content hashes, windows.
- `src/google/calendar` — Google Calendar CRUD, incremental cache, backend.
- `src/google/auth` — Google OAuth2 desktop flow.
- `src/platform/*` — atomic files, locks, retries, disk checks.

## Reconcile

```go
import "github.com/nimakalantar/calendar-agent/src/calendaragent"

actions := calendaragent.Reconcile(calendaragent.Options{
    Window: calendaragent.NewWindow(horizonStart, 30),
    Now:    now,
    Hash:   func(e calendaragent.Event) string { return hashFor(e) },
    Matches: func(e calendaragent.Event, m calendaragent.ManagedEvent) bool {
        return matchesFor(e, m)
    },
    ManualEditPolicy: calendaragent.PreserveManualEdits,
    ObservedHash:     observedHash,
}, desired, existing)
```

The library emits `Create`, `Update`, `Delete`, or `NoOp`. The application
supplies rendered summaries/descriptions, an optional hash/matcher, and a
`Backend`. `Event.Data` and `ManagedEvent.Data` are available for
application-specific values used by those hooks.

## Google Calendar

```go
import (
    "github.com/nimakalantar/calendar-agent/src/calendaragent"
    gcalendar "github.com/nimakalantar/calendar-agent/src/google/calendar"
)

client, _ := gcalendar.NewClientWithOptions(ctx, tokenSource, "", gcalendar.ClientOptions{
    Ownership:     calendaragent.Ownership{Property: "managedBy", Value: "my-agent"},
    MetricsPrefix: "my_agent",
})
backend := gcalendar.NewBackend(client, gcalendar.Config{
    CalendarID: "destination-calendar-id",
    Timezone:   "Europe/London",
    Ownership:  calendaragent.Ownership{Property: "managedBy", Value: "my-agent"},
    Schema:     gcalendar.DefaultSchema(),
})
```

`Client.ListAll` returns unowned events for busy-calendar checks.
`Client.ListRawEvents` exposes provider fields such as recurrence and attendees.

Metrics are optional via the `Inc`/`Set` interface; HTTP instrumentation can be
added with `ClientOptions.WrapTransport`.

## Install

```sh
go get github.com/nimakalantar/calendar-agent/src/calendaragent@latest
```

For local development, use a `replace` in the consuming module.

## Development

```sh
go test ./...
go vet ./...
gofmt -l .
```

Live Google tests require `CALENDAR_AGENT_LIVE_GOOGLE=1` and dedicated test
calendars.

## License

GPL-3.0-only. See [LICENSE](LICENSE).
