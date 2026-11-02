# Photo album
This application is meant to index all the photos and images that are scattered over different 
disks and applications.

The system is composed of trhee systems:

1. A crawler, to get the photos from a file system
1. An indexer, that takes the picture files and creates a multimodal embedding using CLIP
1. A web app, used to search and tag the photos. This web app has a back and a front end.

The photos, during crawling are taken and stored on a S3 like storage system. Ussually I would
use MinIO, but since May 2026 the prject was archived. For thisproject I'll use (SeaweedFS)[https://github.com/seaweedfs/seaweedfs]
which uses the same concepts of Facebook Haystack, it's OSS and it's written in GO.

This project is packaged using Docker and should be able to run in a RaspberryPI (Except for the indexer).

This means, the indexer should be able to run as a separate service that can act during some schedule.

This could be the order:

1. The crawler starts retrieving and copying the files on the S3 storage.
1. Once a file is created, an indexing job is registered on the indexing queue. 
1. The indexer starts picking indexation jobs from the queue
1. Once the indexation is done, the job is marked done and the index is updated.

The job should contain:

```json
{
    "job_id":"UID()",
    "uri": "S3 file path",
    "filename": "The name of the file",
    "created_at": Date,
    "updated_at": Date,
    "status": "queued"|"processing"|"success"|"error",
    "message": "Some message"
}

```

## Web app

The browser UI lives in `phalbum/web`: React 19 + Vite + Tailwind CSS v4 + shadcn/ui, with
TanStack Query for data and wouter for routing. It is a static single-page app: `npm run build`
writes `web/dist`, which is embedded into the `api` binary (`go:embed`), so one process serves both
the API and the app on the same origin.

- Browse by date (grouped by year, with a year jump bar), search by text, by an uploaded picture,
  or "more like this", and filter by tags and dates. All filters live in the URL.
- Tag one photo from the viewer's info panel, or select several (long-press on touch, the
  check mark or shift-click on desktop) and tag them at once. Tags are managed at `/tags`.
- `/admin` is reserved for the future administration UI.

Requires Node 20.19+ (see `web/.nvmrc`).

```sh
cd phalbum
make api        # npm ci + build the web app, then go build -o bin/api ./cmd/api
make dev-web    # Vite on http://localhost:5173 with hot reload; proxies /api to :8090
make gen-api    # regenerate web/src/api/schema.d.ts after changing api/openapi.yaml
```

Without a web build, the API still works and `/` answers 503 with a hint to build the app.

## Seaweedfs
This is the storage layer of the system. The goal of SeaweedFS is to make O(1) disk access. This
is done by the separation of the data and metadata paths.

It has three components:

1. The master: contains file metadata (Volume-to-server mappings). The idea is that the thing is small enough to be cacheable and quick
1. The volume servers (one or more): Store the actual blob data and per-blob metadata
1. Filer: this is a service for exposing POSIX like filesystem abstraction. This is optional.

More information at (DeepWiki - SeaweedFS overview)[https://deepwiki.com/seaweedfs/seaweedfs/1.1-architecture-overview]




Reporting anonymous cluster statistics to https://telemetry.seaweedfs.com/api/collect every 24h0m0s once 10 GiB are stored, use -telemetry=false to opt out
Data is in /data

