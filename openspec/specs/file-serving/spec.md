# file-serving Specification

## Purpose

SlimServe serves one configured root directory over HTTP: a listing page for each directory and a
download for each file. This spec states what is visible and what never leaves the root.

## Requirements

### Requirement: Directory listing

The system SHALL list exactly the entries it would serve whose names are valid UTF-8, folders
first and then by name.

#### Scenario: The listing links exactly the entries a GET serves, folders first

- **WHEN** a directory holds folders, files, a dot-file, an ignored file, a symlink to a file
  inside the root and a symlink to a file outside it
- **THEN** its listing links the folders and then the files, each group in name order
- **AND** the linked entries are exactly the entries whose GET answers 200

#### Scenario: A listed name with reserved characters downloads through its link

- **WHEN** a directory holds files named `a #1%.txt` and `é.txt`
- **THEN** following each link of its listing answers 200 with that file's bytes

### Requirement: Live listing

The system SHALL update a connected listing within a second of a file being added to or removed
from its directory, applying the dot-file and ignore rules.

#### Scenario: A file added to and removed from a viewed directory shows and goes

- **WHEN** a browser shows a directory and a file is written into it, then removed
- **THEN** the listing links the file within a second, and then no longer links it

#### Scenario: A subdirectory's listing shows a file added to it

- **WHEN** a browser shows `/sub` and a file is written into `sub`
- **THEN** the listing links the file within a second

#### Scenario: A dot-file and an ignored file never appear

- **WHEN** a browser shows a directory and a dot-file, a file its `.slimserveignore` names and then a visible file are written into it
- **THEN** the listing links the visible file and neither of the other two

#### Scenario: A viewed directory that is removed sends the browser to its parent

- **WHEN** a browser shows `/sub` and `sub` is removed
- **THEN** the browser navigates to `/` within a second

#### Scenario: A directory reached through a symlink shows a file added to its target

- **WHEN** `link` is a symlink to `sub`, a browser shows `/link` and a file is written into `sub`
- **THEN** the listing links the file within a second

#### Scenario: A viewed directory whose parent is removed sends the browser to the nearest directory left

- **WHEN** a browser shows `/a/b` and `a` is removed
- **THEN** the browser navigates to `/` within a second

### Requirement: File download

The system SHALL send a visible file's bytes with the MIME type of its name, whatever the request's
Accept header, and with the same Content-Security-Policy and `nosniff` headers as every page.
The system SHALL neither list nor serve a file it has no permission to read.

#### Scenario: A file downloads with its bytes and MIME type

- **WHEN** a client requests `/a.txt`
- **THEN** the answer is 200 with the file's bytes and `content-type: text/plain`
- **AND** it carries `x-content-type-options: nosniff` and the `default-src 'self'` policy

#### Scenario: A download ignores the Accept header

- **WHEN** a client requests a file with `Accept: application/json`
- **THEN** the answer is 200 with the file's bytes

#### Scenario: A symlink downloads with the MIME type of its own name

- **WHEN** `x.txt` is a symlink to `x.html`
- **THEN** a GET for `/x.txt` answers `content-type: text/plain`

#### Scenario: A file that cannot be read answers 404 and is not listed

- **WHEN** a file exists and the server has no permission to read it
- **THEN** a GET for it answers 404
- **AND** the listing of its directory does not link it

#### Scenario: A served file is not shadowed by the app's own static files

- **WHEN** the root holds a file whose name the app also ships, such as `favicon.ico`
- **THEN** a GET for it answers with the root's file, because the app's own files live under `/.app`

### Requirement: Containment

The system SHALL answer 404 for any path that resolves outside the root, including through a
symlink or an encoded slash, and for any path with a component that is not valid UTF-8.

#### Scenario: A path with .. segments answers 404

- **WHEN** a client requests `/../etc/passwd`, or a `..` path that names an existing file above the root
- **THEN** the answer is 404

#### Scenario: An encoded slash answers 404

- **WHEN** a path segment contains `%2F`, as in `/sub%2F.env`
- **THEN** the answer is 404

#### Scenario: A NUL byte in a path answers 404

- **WHEN** a client requests `/a.txt%00.html`
- **THEN** the answer is 404

#### Scenario: A name that is not valid UTF-8 answers 404

- **WHEN** a client requests `/%FF.txt` and a file of that name exists, with or without an ignore file above it
- **THEN** the answer is 404

#### Scenario: A symlink that leaves the root answers 404

- **WHEN** a symlink inside the root points outside it, by a relative or an absolute target
- **THEN** a GET for the link, or for a path through it, answers 404

#### Scenario: A relative symlink that stays inside the root is served

- **WHEN** a relative symlink points to a file or a directory inside the root
- **THEN** a GET for the link, or for a file through it, answers 200 with the target's bytes

#### Scenario: The canary downloads when its own directory is the root

- **WHEN** the directory above the test root, which holds the file the traversal tests reach for, is itself the root
- **THEN** a GET for that file answers 200, so those tests cannot pass because the file is missing

### Requirement: Dot-files

The system SHALL neither list nor serve a path with a component starting with ".", whether that
component is in the requested path or in the path a symlink resolves it to.

#### Scenario: A path with a dot component answers 404

- **WHEN** a client requests an existing dot-file, a dot-directory or a file under one
- **THEN** the answer is 404

#### Scenario: A symlink to a dot-file answers 404

- **WHEN** a symlink with a visible name points to a dot-file or a dot-directory inside the root
- **THEN** a GET for the link, or for a path through it, answers 404

### Requirement: Ignore files

The system SHALL neither list nor serve a path that a `.slimserveignore` file on its ancestry
ignores, or that lies under an ignored directory, by its requested path or by the path a symlink
resolves it to, and SHALL treat an ignore file that is unreadable or not valid UTF-8 as ignoring
its whole directory. Patterns are the gitignore subset `*`, `?`, `**`, a leading or inner `/` to
anchor, a trailing `/` for directories only and a leading `!` to negate, where the last matching
pattern wins.

#### Scenario: A nested ignore file hides a file below it only

- **WHEN** `sub/.slimserveignore` names `secret.txt`
- **THEN** `/sub/secret.txt` and `/sub/deep/secret.txt` answer 404
- **AND** `/sub/open.txt` and the root's own `/secret.txt` answer 200

#### Scenario: An ignored directory hides everything under it

- **WHEN** the root's ignore file holds `private/`
- **THEN** `/private` and `/private/x.txt` answer 404
- **AND** a file named `private` answers 200

#### Scenario: A negated pattern serves the file it names

- **WHEN** the root's ignore file holds `*.txt` and then `!keep.txt`
- **THEN** `/keep.txt` answers 200 and `/a.txt` answers 404

#### Scenario: A negated pattern cannot serve a file under an ignored directory

- **WHEN** the root's ignore file holds `private/` and then `!private/keep.txt`
- **THEN** `/private/keep.txt` answers 404

#### Scenario: A deeper ignore file has the last word

- **WHEN** the root's ignore file holds `*.txt` and `sub/.slimserveignore` holds `!keep.txt`
- **THEN** `/sub/keep.txt` answers 200 and `/sub/other.txt` answers 404

#### Scenario: A symlink into an ignored directory answers 404

- **WHEN** the root's ignore file holds `private/` and symlinks with visible names point to `private` and to a file under it
- **THEN** a GET for either link, or for a path through the directory link, answers 404

#### Scenario: An ignore file that is not valid UTF-8 hides its whole directory

- **WHEN** `bad/.slimserveignore` holds bytes that are not valid UTF-8
- **THEN** `/bad` and `/bad/x.txt` answer 404

#### Scenario: An ignore file that starts with a byte-order mark still applies

- **WHEN** the root's ignore file starts with a UTF-8 byte-order mark and then holds `secret.txt`
- **THEN** `/secret.txt` answers 404 and the root's listing does not link it

#### Scenario: A double star inside a name matches like a single star

- **WHEN** an ignore file holds `foo**bar`
- **THEN** `fooxbar` is ignored and `foo/x/bar` is not

#### Scenario: An unreadable ignore file hides its whole directory

- **WHEN** `locked/.slimserveignore` exists and cannot be read
- **THEN** `/locked` and `/locked/x.txt` answer 404
