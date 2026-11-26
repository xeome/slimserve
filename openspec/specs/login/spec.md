# login Specification

## Purpose

An operator can put every route behind one username and password. This spec states what a
browser and any other client see before and after logging in, how logins are rate limited, and
which settings refuse to boot.

## Requirements

### Requirement: Login

With `SLIMSERVE_USERNAME` and `SLIMSERVE_PASSWORD` both set, every route SHALL answer a browser's
GET with a 302 to `/.app/login?next=<path>` and anything else with a bare 401 until the user logs
in, and a live socket without the login SHALL be refused at mount. A login SHALL return to `next`
only when it is a local path, else to `/`. Changing the password SHALL log every session out.

#### Scenario: Every route answers the login page or a refusal until the user logs in

- **WHEN** a client that has not logged in asks for a folder, a file or a thumbnail
- **THEN** a browser is sent to the login page, and any other client gets 401
- **AND** a live socket is sent to the login page

#### Scenario: A refusal carries the content-security-policy header

- **WHEN** a client that has not logged in gets a 401
- **THEN** the answer carries a content security policy

#### Scenario: After logging in, the same browser gets every route

- **WHEN** a browser logs in with the right username and password
- **THEN** it gets folders, files and the live page

#### Scenario: A live socket whose session no longer matches the login is refused at mount

- **WHEN** a logged-in browser loads a page and the password changes before the page connects
- **THEN** the page's live socket is sent to the login page

#### Scenario: A wrong password or a wrong username answers 401 and logs nobody in

- **WHEN** a browser logs in with a wrong password, or a wrong username
- **THEN** the answer is 401 and the browser is still sent to the login page

#### Scenario: A next that leaves the site redirects to /

- **WHEN** a login carries a `next` of `//evil.com`, `https://evil.com` or `/\evil.com`
- **THEN** the login redirects to `/`

#### Scenario: The login form returns the browser to the exact path and query it asked for

- **WHEN** a browser asks for a path with reserved characters and a query, and logs in from the
  form it is sent to
- **THEN** the login redirects to exactly that path and query, which then answers 200

#### Scenario: A session whose password changed is logged out

- **WHEN** the password changes after a browser logged in
- **THEN** the browser's next file request answers 401, and its next page load the login page

#### Scenario: The login page asks for the username once

- **WHEN** a browser loads the login page with login on
- **THEN** the form has one username field

#### Scenario: With login unset, the login and logout paths answer 404 and pages have no logout button

- **WHEN** login is off and no write password is set either
- **THEN** a GET or POST to `/.app/login` and a POST to `/.app/logout` answer 404, behind the
  CSRF check too
- **AND** pages show no logout button

### Requirement: Logout

A logout SHALL send the browser to the login page and close that browser's open pages, a session
whose password changed included.

#### Scenario: Logging out sends the browser back to the login page

- **WHEN** a logged-in browser, whose pages show a logout button, logs out
- **THEN** it is sent to the login page, and so is its next page load

#### Scenario: Logging out from a session whose password changed reaches the login page

- **WHEN** a browser logs out after the password changed
- **THEN** it is sent to the login page

#### Scenario: The login page has no logout button

- **WHEN** a browser loads the login page
- **THEN** the page has no logout button

#### Scenario: Logging out tells that browser's open viewer pages to disconnect

- **WHEN** a browser logged in as a viewer logs out
- **THEN** its open pages are told to disconnect

### Requirement: Login rate limit

The system SHALL answer 429 to every login attempt from a client after 10 failed logins from it
in a rolling minute, the right password included. It SHALL count an IPv6 client by its /64 and an
IPv4 client by its own address, and it MUST hold under parallel attempts.

#### Scenario: The 11th wrong viewer login from one address answers 429, even with the right password

- **WHEN** a client fails to log in 10 times and then sends the right username and password
- **THEN** the answer is 429
- **AND** a client at another address logs in

#### Scenario: Failures count against the whole /64 of an IPv6 address, and not another /64

- **WHEN** an IPv6 client fails 10 times
- **THEN** another address in the same /64 gets 429, and one in another /64 logs in

#### Scenario: An IPv4 client on the dual-stack listener is limited by its own address

- **WHEN** an IPv4 client fails 10 times
- **THEN** another IPv4 client logs in

#### Scenario: 30 parallel wrong attempts from one address get at most 10 answers of 401

- **WHEN** one client sends 30 wrong logins at once
- **THEN** at most 10 answer 401, and the rest 429

### Requirement: Login settings

Login SHALL be on with both `SLIMSERVE_USERNAME` and `SLIMSERVE_PASSWORD` set, and off with both
unset or empty. A server SHALL refuse to boot with only one of them set, or with login on in
production and no `SECRET_KEY_BASE`.

#### Scenario: Login is on when both SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD are set, and off when neither is

- **WHEN** a server starts with both variables set, or with neither
- **THEN** login is on with that username and password, or off

#### Scenario: Only one of SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD refuses to boot

- **WHEN** a server starts with only the username, or only the password
- **THEN** it refuses to boot, naming both variables

#### Scenario: An empty SLIMSERVE_PASSWORD beside a set SLIMSERVE_USERNAME refuses to boot

- **WHEN** a server starts with a username and an empty password
- **THEN** it refuses to boot, naming both variables

#### Scenario: Both login variables empty leave login off

- **WHEN** a server starts with both variables set to empty strings
- **THEN** login is off

#### Scenario: Login in prod without SECRET_KEY_BASE refuses to boot

- **WHEN** a production server starts with login on and no `SECRET_KEY_BASE`
- **THEN** it refuses to boot, naming `SECRET_KEY_BASE`
