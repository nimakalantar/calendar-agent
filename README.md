# calendar-agent

Reusable desired-state reconciliation and a Google Calendar client/backend for
Go applications that manage generated calendar events.

## Packages

- `src/calendaragent` — reconciliation, ownership, content hashes, windows.
- `src/google/calendar` — Google Calendar CRUD, incremental cache, backend.
- `src/google/auth` — Google OAuth2 desktop flow.
- `src/anchor` — location timeline derived from `#anchor` / `#return` events.
- `src/geocode` — coordinates, Nominatim, and cached location lookup.
- `src/location` — shared coordinates.
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

## Location anchors and geocoding

`src/anchor` turns tagged calendar events into a location timeline:

```go
result := anchor.Build(events, "home", 2*time.Hour, persisted, now)
active := anchor.Resolve(result.Timeline, at)
```

`#anchor` moves the active location after the event ends; `#return` / `#home`
reverts to home. `result.State` is the override to persist across runs.

`src/geocode` resolves those location strings. The `auto` strategy tries
coordinates, then UK postcodes via postcodes.io, then rate-limited Nominatim,
with an LRU disk cache:

```go
g := geocode.NewWithOptions(geocode.Options{
    UserAgent: "my-agent/1.0 (+https://example.com)",
    CachePath: ".cache/geocode.json",
    Strategy:  geocode.StrategyAuto,
})
coords, err := g.Resolve(ctx, "55-61 Brewery Rd, London")
defer g.Flush()
```

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
