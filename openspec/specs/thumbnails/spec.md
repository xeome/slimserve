# thumbnails Specification

## Purpose

SlimServe shows a small preview of each image in a listing. This spec states which files get a
thumbnail, how large it is, and how the disk cache behind it behaves.

## Requirements

### Requirement: On-demand thumbnail

The system SHALL answer `GET <path>?thumb=1` with a WebP no larger than the configured size for a
visible file that is a JPEG, PNG, GIF or WebP by both its name and its content, and with 404 for
any other path. The WebP SHALL carry none of the source's metadata. The system SHALL answer 413
for an image file larger than the configured input size, without decoding it.

#### Scenario: Each image format answers a WebP that fits the configured size

- **WHEN** a client requests the thumbnail of a `.JPG`, `.jpeg`, `.png`, `.gif` or `.webp` file
  larger than the configured size, in landscape or portrait
- **THEN** the answer is 200 with `content-type: image/webp`
- **AND** the longer side of the image is the configured size

#### Scenario: An image smaller than the configured size is not enlarged

- **WHEN** a client requests the thumbnail of an image smaller than the configured size
- **THEN** the answer is a WebP with the image's own width and height

#### Scenario: A path that is not an image answers 404

- **WHEN** a client requests the thumbnail of a text file, a directory, a missing path, or a file
  with an image extension that does not decode
- **THEN** the answer is 404

#### Scenario: A file that holds another format than its name says answers 404

- **WHEN** a client requests the thumbnail of an SVG file named `.png` that references a hidden
  image beside it
- **THEN** the answer is 404

#### Scenario: A thumbnail carries none of the source's metadata

- **WHEN** a client requests the thumbnail of a JPEG that carries EXIF
- **THEN** the WebP in the answer carries no EXIF

#### Scenario: An ignored image answers 404 while its twin answers 200

- **WHEN** a `.slimserveignore` file names one of two images in a directory
- **THEN** the thumbnail of the ignored image answers 404
- **AND** the thumbnail of the other answers 200

#### Scenario: An image over the input size limit answers 413

- **WHEN** a client requests the thumbnail of an image file larger than the configured input size
- **THEN** the answer is 413, whatever the file holds

#### Scenario: The listing shows a thumbnail for each image it can make one for

- **WHEN** a directory holds an image, a text file and an image over the input size limit
- **THEN** its listing shows one thumbnail, for the image, and its URL answers a WebP

### Requirement: Thumbnail cache

The system SHALL serve a repeated thumbnail from disk, make a new one when the source file
changes, and keep the cache under its configured size by deleting the oldest thumbnails. The cache
SHALL delete only files named like its own. The system SHALL still answer thumbnails when the
cache process is down or the cache directory cannot be made.

#### Scenario: A repeated request is served from the cache

- **WHEN** a client requests the same thumbnail twice
- **THEN** the first answer carries `x-cache: miss` and the second `x-cache: hit`
- **AND** both carry the same bytes

#### Scenario: A changed source file misses the cache

- **WHEN** an image is replaced after its thumbnail was cached
- **THEN** the next request answers `x-cache: miss` with a thumbnail of the new image

#### Scenario: A replacement with the same size and time misses the cache

- **WHEN** another image of the same byte size and modification time is moved onto a cached image
- **THEN** the next request answers `x-cache: miss` with a thumbnail of the new image

#### Scenario: A thumbnail is served while the cache process is down

- **WHEN** the cache process is not running
- **THEN** a thumbnail cached before answers `x-cache: hit`
- **AND** one not cached answers `x-cache: miss`

#### Scenario: A put past the cap deletes the oldest thumbnails

- **WHEN** storing a thumbnail takes the cache over its configured size
- **THEN** the thumbnails with the oldest file times are deleted from disk until the cache is under it
- **AND** the thumbnail just stored stays

#### Scenario: A restart keeps the cache and its cap

- **WHEN** the server starts on a cache directory that holds thumbnails and an unfinished write
- **THEN** the unfinished write is deleted
- **AND** the thumbnails already there count toward the configured size

#### Scenario: Files the cache did not write are left alone

- **WHEN** the cache directory holds a `.tmp` and a `.webp` file not named like a cache key
- **THEN** both are still there after a start and after an eviction

#### Scenario: A cache directory that cannot be made leaves the server without a cache

- **WHEN** the server starts and the cache directory cannot be made
- **THEN** the cache still starts
- **AND** a stored thumbnail is dropped and a lookup misses
