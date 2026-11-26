# s3-storage Specification

## Purpose

SlimServe can serve a private bucket of any S3-compatible store, MinIO included, instead of a
folder on disk. This spec states the settings, how a bucket lists and downloads, how the server
keeps the store's work low, and how writes behave on a store. How fresh a listing is across nodes,
and after an edit made in the bucket directly, is the system spec's.

## Requirements

### Requirement: Serving a bucket

With `SLIMSERVE_STORAGE=s3` a server SHALL serve `SLIMSERVE_S3_BUCKET` with `AWS_ACCESS_KEY_ID`
and `AWS_SECRET_ACCESS_KEY`, all required, `AWS_REGION` (default `us-east-1`),
`AWS_SESSION_TOKEN`, and `AWS_ENDPOINT_URL_S3` for a store other than AWS, and needs no root.
Requests SHALL be path-style, so the endpoint names the store, not the bucket. Local storage MUST
stay the default.

#### Scenario: An S3 server needs no root, and takes its bucket and the standard AWS variables

- **WHEN** a server starts with `SLIMSERVE_STORAGE=s3`, a bucket, an endpoint and credentials, and
  no root
- **THEN** it serves that bucket from that endpoint with those credentials, in `us-east-1`

#### Scenario: An S3 server without a bucket or credentials, or an unknown backend, refuses to boot

- **WHEN** a server starts on S3 without `SLIMSERVE_S3_BUCKET` or `AWS_SECRET_ACCESS_KEY`, or with
  `SLIMSERVE_STORAGE` naming an unknown backend
- **THEN** it refuses to boot, naming the variable

#### Scenario: A local server is still the default and still needs an existing root

- **WHEN** a server starts with no `SLIMSERVE_STORAGE`
- **THEN** it serves local storage
- **AND** a production server started with `PHX_SERVER` refuses to boot when `SLIMSERVE_ROOT`
  names a missing path

#### Scenario: A bucket that redirects to its regional host answers 502

- **WHEN** a bucket on AWS lives in another region and `AWS_ENDPOINT_URL_S3` is unset, so requests
  go to the global host
- **THEN** every request answers 502, until the endpoint names the bucket region's host

### Requirement: Bucket listing

A bucket listing SHALL show folders first, each folder once even where a key is both an object
and a prefix, and drop each folder's own marker. `.slimserveignore` objects and hidden names SHALL
work as on disk, and a key whose name is not valid UTF-8, or holds a NUL, SHALL not be listed.

#### Scenario: A bucket lists folders first and drops each folder's own marker

- **WHEN** a bucket holds files, a name with a space and a plus, folders with markers and an
  empty folder
- **THEN** the root lists the folders first and then the files, and each folder lists no marker
- **AND** each file downloads through its link

#### Scenario: A bucket hides dot-names, ignored paths and invalid names, and refuses them by URL

- **WHEN** a bucket holds an ignore object, dot-names, an ignored folder and file, and a visible file
- **THEN** the root lists only the visible file
- **AND** each hidden path, a NUL path, a missing path and an encoded slash answer 404

#### Scenario: A name that is both an object and a prefix is listed once, as a folder

- **WHEN** the store lists one name as both an object and a prefix
- **THEN** the listing shows it once, as a folder

#### Scenario: A bucket ignore file that is not valid UTF-8 hides its folder

- **WHEN** a folder's ignore object is not valid UTF-8
- **THEN** the folder is not listed and its files answer 404

#### Scenario: A bucket listing follows the store past its first page

- **WHEN** a folder holds 1001 objects
- **THEN** it lists all of them in order, and the last downloads

#### Scenario: The tree and the card show a bucket's folders and files, and a change lists no unchanged subfolder again

- **WHEN** a browser opens a bucket folder
- **THEN** the tree and the "In this folder" card count its visible entries as on disk
- **AND** after a change in that folder, the bucket gets no listing request for an unchanged
  subfolder or sibling

#### Scenario: On a bucket, an unfolded level drops a subfolder another page deletes

- **WHEN** a page unfolds a bucket folder and another page deletes one of its subfolders
- **THEN** the tree drops that subfolder within a second

#### Scenario: On a bucket, an image's kind line carries its pixel size once its thumbnail is made

- **WHEN** a guest selects a bucket PNG before and after its thumbnail is made
- **THEN** the kind line reads "PNG image" first, and then "PNG image · 200 × 100"

### Requirement: Download cache

Files up to 100 MB SHALL be kept in `SLIMSERVE_DOWNLOAD_CACHE_DIR`, up to
`SLIMSERVE_DOWNLOAD_CACHE_MB` (default 1024), once fetched, so a repeat download, ranged or not,
reads no bytes from the store. Larger files, and misses the cache has no room for, SHALL stream
through, and a HEAD SHALL fetch no body.

#### Scenario: A small file is fetched once, then every full and ranged answer comes from the cache

- **WHEN** a client downloads a small file, and then asks for it whole, by ranges, and past its end
- **THEN** the first answer misses with the right type and `Accept-Ranges: bytes`
- **AND** every later answer is right and comes from the cache, with no GET to the store

#### Scenario: A miss the cache has no room for streams, with the normalized range pinned to the ETag

- **WHEN** the cache is full and a client asks for `bytes=-3` and then the whole file
- **THEN** both answers stream, right, with their length and range
- **AND** the store gets a path-style GET for `bytes=7-9` with `If-Match` on the object's ETag

#### Scenario: A HEAD that would stream answers with GET's status and headers but fetches no body

- **WHEN** the cache is full and a client sends a HEAD, whole or ranged, for a file
- **THEN** it gets the GET's status and headers with no body, and the store gets no GET

#### Scenario: A HEAD on a cacheable file fetches and caches nothing on a miss, and answers a hit from the cache

- **WHEN** a client sends a HEAD for an uncached small file, and again after a GET cached it
- **THEN** the first HEAD gets the GET's status and headers and its length, with no GET to the
  store, and the later GET still misses
- **AND** the second HEAD answers from the cache

#### Scenario: A streamed answer with the wrong length, range or ETag is refused before any header

- **WHEN** a streamed answer from the store carries a length, a range or an ETag other than asked
- **THEN** the client gets 502, with no range, length or cache header

#### Scenario: A streamed body cut short closes an HTTP/1 connection and resets an HTTP/2 stream

- **WHEN** the store's body ends before its length
- **THEN** an HTTP/1 client sees the connection close, and an HTTP/2 client sees its stream reset,
  each after a 200 and part of the body

#### Scenario: A streamed HEAD sends GET's status and length over HTTP/1 and HTTP/2 but fetches no body

- **WHEN** the cache is full and a client sends a HEAD, whole or ranged, over HTTP/1 or HTTP/2
- **THEN** it gets the GET's status and length on the wire with no body, and the store gets no GET

#### Scenario: An empty download cache directory takes the default

- **WHEN** `SLIMSERVE_DOWNLOAD_CACHE_DIR` is set empty
- **THEN** the cache lives in its default folder under the system's temporary folder

#### Scenario: A kept file is served from the cache with its bytes, and a file never kept misses

- **WHEN** the cache holds one file
- **THEN** a download of it reads its bytes from the cache folder
- **AND** a download of any other file misses

#### Scenario: While one download fills the cache, another miss gets no room, and gets room once the first ends

- **WHEN** one download holds the whole cache, another file misses, and then the first download ends
- **THEN** the other miss gets no room in the cache
- **AND** a miss after the first download ends gets room

#### Scenario: A download that ends early leaves no partial file, and a file the size of the cache still fits

- **WHEN** a download holding room in the cache ends before it is done
- **THEN** no partial file stays in the cache folder
- **AND** a file as large as the whole cache can still be kept

#### Scenario: Eviction takes the least recently downloaded file, and never one being sent

- **WHEN** the cache is full and another file is fetched
- **THEN** the file least recently downloaded goes, but never one a client is still receiving

#### Scenario: A file larger than the whole cache is not kept and evicts nothing

- **WHEN** a file is larger than the whole cache
- **THEN** it is not kept, and the cache keeps every file it had

#### Scenario: A cache directory removed from outside is made again

- **WHEN** the cache folder is deleted while the server runs, and a file is fetched
- **THEN** the folder is made again, and a file can be written there again

#### Scenario: Two misses of one file at once keep one copy, and a file that fills the rest still fits

- **WHEN** two clients miss on the same file at once and both keep it
- **THEN** the cache serves one copy of it
- **AND** a file that fills the rest of the cache can still be kept

#### Scenario: The download cache defaults to 1024 MB

- **WHEN** `SLIMSERVE_DOWNLOAD_CACHE_MB` is unset
- **THEN** the cache holds up to 1024 MB

### Requirement: Bucket thumbnails

Thumbnails of bucket images SHALL behave as on disk. A thumbnail HEAD SHALL render nothing on a
miss, and an image too big to cache SHALL answer 503 with no fetch.

#### Scenario: A thumbnail HEAD renders nothing on a miss and sends no length, and a hit's length

- **WHEN** a client sends a thumbnail HEAD over HTTP/1 and HTTP/2, then a GET, then a HEAD again
- **THEN** the first HEAD answers 200 WebP with no length and no fetch of the image
- **AND** the GET misses, and the later HEAD hits with the GET's length and no fetch

#### Scenario: A thumbnail HEAD for an image too big to cache answers the GET's 503 and fetches nothing

- **WHEN** a bucket image is over 100 MB and under the thumbnail input limit
- **THEN** a HEAD and a GET of its thumbnail both answer 503, and the image is never fetched

#### Scenario: A replaced image gets a new thumbnail URL and the new thumbnail

- **WHEN** a writer deletes a bucket image whose thumbnail is cached and uploads another at its name
- **THEN** the listing shows a new thumbnail URL, which misses and shows the new image

### Requirement: Listing cache

The server SHALL keep each folder's listing and ignore rules and ask the store again only for
what a write through SlimServe changed. An open page SHALL show a write from its first update
after it, SHALL add no request to the store, idle or after a write, and SHALL keep its rows while
the store fails.

#### Scenario: A listing read before a change is not served after it

- **WHEN** a change lands while a listing is being read from the store
- **THEN** the next listing shows the change

#### Scenario: A fresh listing reads each folder's ignore object once

- **WHEN** a folder is listed with nothing cached
- **THEN** the store gets one read of each ignore object on its path and in its subfolders

#### Scenario: The bucket gets both subfolders' ignore-object reads before it answers either

- **WHEN** a folder with two subfolders is listed
- **THEN** the bucket gets the read of each subfolder's ignore object before it answers either

#### Scenario: After an upload, another folder lists again with no request to the bucket

- **WHEN** a writer uploads into one folder
- **THEN** listing another folder shows its file, and looking that file up again finds it, with no
  request to the bucket

#### Scenario: An upload asks the store again for its own folder's listing only

- **WHEN** a writer uploads into a folder two levels down
- **THEN** the store gets one listing request, for that folder alone, and none for its ancestors

#### Scenario: A delete that empties an implicit folder leaves it absent with no further request

- **WHEN** a writer deletes the last file of a folder that has no marker
- **THEN** its parent no longer lists the folder, with no further request to the store

#### Scenario: A folder emptied just before an upload into it lists again once the upload lands

- **WHEN** a folder's last file is deleted just before an upload into it lands
- **THEN** the parent lists the folder again, and the folder resolves

#### Scenario: A folder move makes the names under its destination resolve

- **WHEN** a name under a folder was looked up and missing, and a folder is moved there
- **THEN** that name resolves to the moved file

#### Scenario: After an upload wakes a page, its first listing shows the new file with no request to the bucket

- **WHEN** a writer uploads into a folder an open page shows
- **THEN** the store gets that folder's listing request before the page is woken
- **AND** the first listing after the wake shows the new file and the old one, with no request to
  the bucket

#### Scenario: A burst of uploads into one folder gets one listing request, and the open page shows all three

- **WHEN** a writer uploads three files at once into a folder an open page shows
- **THEN** the store gets one listing request for that folder, and the page lists all three

#### Scenario: After a malformed listing answer, the folder is listed from the store again and the server keeps serving

- **WHEN** the store answers a folder's listing malformed after a write
- **THEN** the server keeps serving, and the next listing of that folder asks the store again and
  shows every file in it

#### Scenario: An open bucket page shows a write and adds no request to the bucket

- **WHEN** a writer uploads into a folder an open page shows
- **THEN** the page lists the new file
- **AND** the bucket gets no request beyond those the write itself makes

#### Scenario: While a bucket page sits open and idle, the bucket gets no request

- **WHEN** a bucket page sits open and idle with nothing cached
- **THEN** the bucket gets no request

#### Scenario: An open bucket page shows an upload whose answer was lost

- **WHEN** an upload lands in the store but its answer never comes back
- **THEN** the upload reports a failure, and the open page and the next listing show the file

#### Scenario: An open bucket page drops a file whose delete's answer was lost

- **WHEN** a delete lands in the store but its answer never comes back
- **THEN** the delete reports a failure, and the open page and the next listing drop the file

#### Scenario: An open bucket page keeps its rows while the bucket fails

- **WHEN** the store starts failing right after an upload, and the page lists again
- **THEN** the page keeps the rows it had

#### Scenario: An open bucket page lists a folder it could not read again once the store recovers

- **WHEN** a page could not list its folder because the store failed, and the store recovers
- **THEN** the page lists the folder, the new file included

#### Scenario: A delete of the open bucket folder sends the page to the nearest folder

- **WHEN** a page shows a bucket folder and the folder is deleted
- **THEN** the page navigates to `/`

#### Scenario: A store error while finding the nearest folder keeps the rows

- **WHEN** a page's folder seems gone and the store fails while the page looks for the nearest folder
- **THEN** the page keeps its rows

#### Scenario: A write in the open folder counts none of its unchanged subfolders again

- **WHEN** a writer uploads into a folder an open page shows, with nothing cached
- **THEN** the store is asked for no listing of its subfolders or of other folders

#### Scenario: A change made while the page lists its folder still reaches the page

- **WHEN** a change lands right after a page's own listing of its folder comes back from the store
- **THEN** the page shows the change

### Requirement: Conditional-write check

With `SLIMSERVE_WRITE_PASSWORD` set, the server SHALL first write and remove a few objects under
a hidden prefix of the bucket to learn which conditional writes the store
enforces. Uploads and new folders SHALL be refused, with a warning at boot, unless the store
refuses to overwrite an existing key. Where it ignores a stale `If-Match`, move and delete SHALL
check the object themselves just before acting.

#### Scenario: The start-up check removes every version and upload it made, including those whose answer was lost

- **WHEN** the check runs on a versioned bucket and the answers to one write and one upload are lost
- **THEN** the bucket keeps no version and no open upload the check made
- **AND** the lost write still proves the store refuses overwrites, so uploads stay allowed, and
  the lost upload proves nothing

#### Scenario: A start-up check write that never lands fails the boot

- **WHEN** the store answers every write of the check with 503
- **THEN** the check fails, the server learns no guarantee, and the boot fails

#### Scenario: Without the no-clobber guarantees, upload and new folder are refused, and move and delete still work

- **WHEN** the store answers a conditional write it did not honour with 200
- **THEN** uploads and new folders are refused with an error and the bucket is unchanged
- **AND** moves and deletes still work

#### Scenario: An upload or a new folder onto a name taken after the fresh check is refused, and the winner stays

- **WHEN** another writer takes the name right after an upload's or a new folder's last check
- **THEN** the upload shows an error, and the other writer's file and folder stay

### Requirement: Bucket writes

Writes on a bucket SHALL behave as on disk. A move SHALL be a copy then a delete, key by key, and
files over 5 GiB SHALL copy in 5 GiB parts. A move or delete that fails part-way SHALL say so and
leave what it did.

#### Scenario: On a bucket, a writer uploads, makes a folder, moves a file there and back and deletes it, and another page follows

- **WHEN** a writer uploads a file, makes a folder, moves the file into it, moves it back and
  deletes it
- **THEN** the bucket holds each result in turn, with no error
- **AND** a second open page lists each result within a second

#### Scenario: A folder moves key by key to the exact new keys, markers and dot-files included

- **WHEN** a writer moves a folder into itself, and then into a sibling whose name starts the same
- **THEN** the first is refused with an error
- **AND** every key of the folder, markers, dot-files and names with spaces and plus signs
  included, is at the new prefix and nowhere else

#### Scenario: A folder holding a file its ignore rules hide does not move, even when the cached rules are older

- **WHEN** an ignore object that hides a file in a folder appears after the page listed, and a
  writer moves the folder
- **THEN** the bucket is unchanged

#### Scenario: A move uses the entry's kind now, not the kind the page listed

- **WHEN** a page listed a name as a file, the name became a folder, and a writer moves it to a
  name off the upload type list
- **THEN** the folder moves

#### Scenario: A bucket folder with files is deleted only after the bar that counts them, and asks again after a change

- **WHEN** a writer deletes an empty folder, and then a folder with two files
- **THEN** the empty folder goes at once, and the other asks with a count of 2
- **AND** a file added before the confirm makes the bar ask again with 3, and a second confirm
  deletes everything

#### Scenario: A folder holding only empty folders is deleted only after the bar

- **WHEN** a writer deletes a folder that holds only empty folders
- **THEN** a confirm bar shows and the bucket is unchanged

#### Scenario: A folder delete that fails for one key reports a partial outcome and keeps that key

- **WHEN** a folder delete is confirmed and the store refuses one of its keys
- **THEN** that key stays, the rest go, and the page says part of the change was refused

#### Scenario: A file over 5 GiB moves in 5 GiB parts

- **WHEN** a writer moves a file over 5 GiB
- **THEN** it is copied in parts of at most 5 GiB, and then the old key is deleted
