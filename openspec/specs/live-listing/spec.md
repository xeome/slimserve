# live-listing Specification

## Purpose

An open listing page follows its folder without a reload. This spec states how fast it follows,
which changes it shows, and where it goes when its folder disappears.

## Requirements

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

#### Scenario: A file that sorts first leaves each existing row on its own entry

- **WHEN** a browser shows a folder and a file that sorts before the others is added to it
- **THEN** after the relist each existing row still shows its own entry, so no row is patched with
  another entry's content
