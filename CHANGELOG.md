# Changelog

## v2.2.0

Bug-fix release. Backward compatible with v2.1.0: no exported identifier or
signature is added, removed or changed (`apidiff -m` against v2.1.0 reports
no changes). No dependency changes.

### Fixed

- Serialise writes per connection (fixes concurrent write panics and corrupt
  frames). gorilla/websocket allows one concurrent writer per connection, but
  `Connection.Emit` (and so `Broadcast`, `BroadcastToRoom`,
  `BroadcastToRoomChannel` and `Session.Emit`) and the keepalive ping wrote
  from several goroutines without a lock. Under load (for example, one user
  with several tabs in the same room) this caused recovered
  `concurrent write to websocket connection` panics, corrupt or empty frames,
  and dropped connections (`RSV1 set`, `bad opcode`). An unrecovered panic in
  the ping goroutine could also crash the process.
  - `Connection.Emit` takes a new per-connection write lock. It is separate
    from the `Connection` RWMutex that guards `Data`, so `SetData` and
    `GetData` never wait behind a write.
  - The ping uses `WriteControl`, which gorilla/websocket documents as safe
    alongside other writes, with a `Config.WriteWait` deadline.
- `Session.Emit` and `Session.HasSession` no longer read the session's
  connection map while `UpdateSession` or a disconnect changes it (a data
  race that could end in `concurrent map iteration and map write`). They
  copy or read it under the session lock, and the disconnect path now takes
  that lock when it removes the connection.
- `Sockets.CheckIfSessionExists` reads the session registry under the
  registry read lock. Before, a call during `AddSession`, `DeleteSession` or
  a disconnect could end the process with
  `fatal error: concurrent map read and map write`, which `recover` cannot
  catch.

### Changed

- `Config.WriteWait` (default 10s) is now applied. It was documented as "time
  allowed to write a message to the peer" but was not used, so a write to a
  peer that stopped reading could block forever. Each `Emit` and ping now has
  that deadline. After a write times out, gorilla/websocket fails every later
  write on the connection, so the next ping fails and the connection is
  closed and cleaned up as for any other ping failure (within one
  `PingPeriod`). `Emit` clears the deadline after each write, so it does not
  leak into anything else. Zero still means the default; a negative
  `WriteWait` disables the deadline (the v2.1.0 behaviour).
- `Broadcast`, `BroadcastToRoom` and `BroadcastToRoomChannel` pick their
  target connections under the registry read lock and write after releasing
  it. A slow peer no longer holds the registry lock, which used to block
  `JoinRoom`, `LeaveRoom`, session changes and disconnect cleanup for every
  connection. A connection that disconnects between selection and write gets
  a failed write, which is logged at Warn as before.
- `Session.Emit` writes to the session's connections after releasing the
  session lock.

Lock order: `Sockets` registry lock, then a `Session` lock; the per-connection
write lock is never held while either is taken.

### Notes for consumers

- Write to a connection only through `Emit` (or the broadcast and session
  helpers). Writing to the exported `Connection.Conn` directly bypasses the
  write lock and can still panic with `concurrent write to websocket
  connection`.
- `Session.HasSession`, `Session.Emit` and the disconnect path now take the
  `Session`'s embedded mutex, which is not re-entrant. Do not call them, or
  any `Sockets` method, while holding `ctx.Session.Lock()`.
- Broadcasts still write to their targets one after another on the caller's
  goroutine, so a peer that has stopped reading delays that call (and the
  targets after it) by up to `WriteWait` once. Later writes to it fail at
  once until it is closed.

## v2.1.0

Maintenance and security release. Backward compatible with v2.0.0: no
exported identifier or signature is removed or changed (checked with
`apidiff`; the only changes are additions).

### Security

- `github.com/gorilla/websocket` v1.5.0 -> v1.5.3. Fixes
  [GO-2026-6278](https://pkg.go.dev/vuln/GO-2026-6278) (weak PRNG for the
  client mask key), which v2.0.0 called.
- `github.com/gin-gonic/gin` is no longer a dependency. It was only used by
  `examples/server`, which now uses `net/http`. This drops gin's indirect tail
  (old `golang.org/x/crypto`, `golang.org/x/sys`, `gopkg.in/yaml.v2`,
  `github.com/golang/protobuf`, `github.com/ugorji/go`, validator,
  json-iterator, ...) from the module graph.
- `github.com/rs/xid` v1.4.0 -> v1.6.0.
- `govulncheck ./...` (go1.25.9) reports no third-party advisories, called or
  not. The remaining findings are in the Go standard library and are fixed by
  building with a current Go patch release.

### Added

- `Config.Logger *slog.Logger`: optional logger for the server. When nil the
  library logs to `slog.Default()`, resolved at log time.
- `client.Secure.Logger *slog.Logger`: the same for the client.
- `common.ParseJWT(token, key) (JWT, error)`: the same checks as `DecodeJWT`,
  but returns why a token was rejected.
- `common.ErrEmptyKey`, `common.ErrMissingUsername`, `common.ErrInvalidToken`:
  sentinel errors for `errors.Is`. Rejections other than an empty key wrap
  `ErrInvalidToken` and the `jwt/v5` cause (for example `jwt.ErrTokenExpired`,
  `jwt.ErrTokenSignatureInvalid`, `jwt.ErrTokenMalformed`).

### Changed

- The library no longer writes to the stdlib `log` package or to stdout. All
  messages go through the logger above, as structured `log/slog` records:
  - failures (websocket upgrade, emit, close, invalid input, invalid `realIP`,
    protected event called without a session) at **Warn**;
  - shutdown on interrupt at **Info**;
  - connection, room, channel and session lifecycle messages, and events with
    no registered handler, at **Debug**.

  With the default `slog.Default()` handler, Info and above are printed
  through the stdlib `log` output as before, but the Debug lifecycle messages
  are no longer printed unless the consumer enables Debug level. Failures
  that are also returned as errors (`UpdateSession`, `DeleteSession` with an
  unknown user) are logged at Debug only.
- `common.DecodeJWT` no longer logs `empty signing key` / `missing username`.
  Its behaviour and signature are unchanged; use `ParseJWT` for the reason.
- The ping/pong read-deadline errors are now checked: a failure to extend the
  read deadline in the pong handler closes the connection rather than being
  ignored.
- Removed the unused unexported `Sockets.removeConnection` and
  `Session.removeConnection`.
- Removed the prebuilt Linux binaries `examples/client/client` and
  `examples/server/server` from the repository (they were built with the old
  dependencies); `go build ./examples/...` builds them.

## v2.0.0

Security release. Replaces `github.com/golang-jwt/jwt` v3 (affected by
[GO-2025-3553](https://pkg.go.dev/vuln/GO-2025-3553), excessive memory
allocation during JWT header parsing, no fix available on v3) with
`github.com/golang-jwt/jwt/v5` v5.3.1, and hardens JWT validation.

This is a major version: the module path and several `common` package types
change. Migration steps are below.

### Breaking changes and migration

1. **Import path moves to `/v2`.**
   ```
   go get github.com/syleron/sockets/v2@v2.0.0
   ```
   Rewrite imports:
   - `github.com/syleron/sockets` -> `github.com/syleron/sockets/v2`
   - `github.com/syleron/sockets/client` -> `github.com/syleron/sockets/v2/client`
   - `github.com/syleron/sockets/common` -> `github.com/syleron/sockets/v2/common`

   Package names (`sockets`, `client`, `common`) are unchanged.

2. **`common.JWT` embeds `jwt.RegisteredClaims` instead of `jwt.StandardClaims`**
   (jwt v3 -> v5). Replace `j.StandardClaims` with `j.RegisteredClaims`. Field
   types change:
   - `ExpiresAt`, `NotBefore`, `IssuedAt`: `int64` -> `*jwt.NumericDate`. Build
     them with `jwt.NewNumericDate(t)` and read them with `.Time` (check for `nil`
     first; an absent claim is `nil`, not `0`).
   - `Audience`: `string` -> `jwt.ClaimStrings` (`[]string`).
   - `Id` -> `ID`.

   Import `github.com/golang-jwt/jwt/v5` for these types.

3. **`common.DecodeJWTNoVerify` returns jwt v5 `jwt.MapClaims`.** The underlying
   type is still `map[string]interface{}`, so indexing code compiles unchanged;
   code that names the type must import `github.com/golang-jwt/jwt/v5`. The
   result is unverified and must not be used for authentication.

4. **`common.GenerateJWT` sets the `username` claim instead of `id`.** Tokens
   it produces are now accepted by `common.DecodeJWT` (v1 tokens were always
   rejected, because `DecodeJWT` requires `username`). Consumers that read the
   `id` claim from these tokens must read `username`.

5. **`common.DecodeJWT` only accepts HS256.** The token's `alg` header is no
   longer trusted: `none`, HS384, HS512 and all asymmetric algorithms (RS*, ES*,
   PS*, EdDSA) are rejected. Tokens signed with any other algorithm must be
   re-issued as HS256.

6. **Minimum Go version is 1.21** (`go` directive raised from 1.18; required
   by jwt v5).

### Other behaviour changes

- `DecodeJWT` rejects a token whose `iat` is in the future, in addition to the
  existing `exp` / `nbf` checks. There is no leeway. `exp`, `nbf` and `iat`
  remain optional, as in v1.
- `DecodeJWT` rejects an empty `tokenKey`, and `GenerateJWT` returns an error
  for an empty `secret`, rather than signing or verifying with an empty HMAC key.
- The username check is now a jwt v5 `ClaimsValidator` (`JWT.Validate`), run
  only after the signature has been verified.

### Added

- Unit tests for `common` JWT handling: valid, expired, not-yet-valid, future
  `iat`, bad signature, tampered payload, stripped signature, HS384/HS512,
  `alg: none`, RS256, HS256-with-public-key confusion, missing username, empty
  key, malformed input to `DecodeJWT` and `DecodeJWTNoVerify`, and a
  `GenerateJWT` -> `DecodeJWT` round trip.

### Known issues (not addressed in this release)

`govulncheck` still reports advisories in dependencies this release does not
bump: `github.com/gorilla/websocket` v1.5.0 (GO-2026-6278, called; fixed in
v1.5.3), `github.com/gin-gonic/gin` v1.7.7 (used by the example server),
and the old `golang.org/x/crypto` / `golang.org/x/sys` pins (not called).
