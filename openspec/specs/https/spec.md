# https Specification

## Purpose

SlimServe speaks plain HTTP and leaves TLS to a proxy in front of it. This spec states what it
serves by default, and what changes when an operator turns HTTPS enforcement on behind such a
proxy.

## Requirements

### Requirement: Plain HTTP by default

A production server SHALL serve plain HTTP on `PORT` by default, and its links SHALL name that
scheme and port. Turning HTTPS on MUST NOT change the port it listens on.

#### Scenario: Production advertises the URL for each mode without changing the HTTP listener

- **WHEN** a production server starts on a port with HTTPS off, and then with HTTPS on for a host
- **THEN** with HTTPS off its links name http and that port
- **AND** with HTTPS on its links name https, the host and port 443, and it still listens on that port

### Requirement: HTTPS enforcement

With `SLIMSERVE_HTTPS=true` and `PHX_HOST` set to the hostname clients use, the server SHALL
redirect plain HTTP to that host over HTTPS, trust a proxy's forwarded HTTPS, accept a live socket
only from the origin the request itself names, and mark the login cookie secure. A request to `localhost` or `127.0.0.1`
MUST NOT be redirected. A production server SHALL refuse to boot with HTTPS on and no `PHX_HOST`,
or with `SLIMSERVE_HTTPS` set to anything but true or false.

#### Scenario: With HTTPS on, plain HTTP redirects to the canonical HTTPS host, the live socket path too

- **WHEN** HTTPS is on and a client asks for a folder with a query, or the live socket path, over
  plain HTTP on another host name
- **THEN** the answer is a 301 to the same path and query on the configured HTTPS host

#### Scenario: A forwarded HTTPS WebSocket origin passes the origin check

- **WHEN** HTTPS is on and a proxy forwards a WebSocket upgrade from HTTPS on the configured host
- **THEN** the socket opens for that origin, and another origin gets 403

#### Scenario: The login cookie follows the HTTPS policy even for localhost HTTP

- **WHEN** a browser logs in over plain or forwarded HTTPS with HTTPS off, and then with HTTPS on
- **THEN** the login cookie is not secure with HTTPS off
- **AND** it is secure with HTTPS on, for a localhost request over plain HTTP too

#### Scenario: HTTPS requires a nonempty PHX_HOST

- **WHEN** a production server starts with HTTPS on and `PHX_HOST` unset or empty
- **THEN** it refuses to boot, naming `PHX_HOST`

#### Scenario: A nonempty SLIMSERVE_HTTPS value other than true or false refuses to boot

- **WHEN** a production server starts with `SLIMSERVE_HTTPS` set to `1`, `TRUE`, `yes` or `on`
- **THEN** it refuses to boot, naming `SLIMSERVE_HTTPS`
