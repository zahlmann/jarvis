# Parallel Search + Extract

Distilled for Jarvis from Parallel's official docs on 2026-03-19.

Assume `PARALLEL_API_KEY` is already present in env. Do not ask the user to paste it again.

## Use This First

- Use `search` when you need live web research across the public web.
- Use `extract` when you already have one or more URLs and want clean markdown from those pages.
- Prefer the local CLI wrappers before hand-writing raw HTTP calls.

## Jarvis CLI

Search the web:

```bash
./bin/jarvisctl parallel search --objective "latest postgres 18 release notes"
```

Extract focused excerpts from a known page:

```bash
./bin/jarvisctl parallel extract \
  --url https://www.postgresql.org/docs/release/18.0/ \
  --objective "major changes in postgres 18"
```

Extract full markdown for crawling/reading:

```bash
./bin/jarvisctl parallel extract \
  --url https://www.postgresql.org/docs/release/18.0/ \
  --full-content
```

Pass advanced request JSON directly when needed:

```bash
./bin/jarvisctl parallel search --payload-file scratch/parallel-search.json
./bin/jarvisctl parallel extract --payload '{"urls":["https://example.com"],"excerpts":true,"full_content":true}'
```

## Raw API

Base URL:

```text
https://api.parallel.ai
```

Required headers:

```text
Content-Type: application/json
x-api-key: $PARALLEL_API_KEY
```

### Search

Endpoint:

```text
POST /v1beta/search
```

Smallest useful body:

```json
{
  "objective": "latest postgres 18 release notes"
}
```

Useful optional fields confirmed in the official quickstart:

- `search_queries`: array of supporting keyword queries
- `mode`: e.g. `"fast"`
- `excerpts`: object such as `{"max_chars_per_result": 10000}`
- `max_results`: upper bound, not a guaranteed count

Typical response shape:

```json
{
  "search_id": "search_...",
  "results": [
    {
      "url": "https://example.com",
      "title": "Example",
      "publish_date": "2025-01-15",
      "excerpts": ["..."]
    }
  ],
  "warnings": null,
  "usage": [{"name": "sku_search", "count": 1}]
}
```

Use search when:

- You need to discover sources.
- You need ranked URLs plus excerpts.
- The user asks for current information and you do not already have the target page.

### Extract

Endpoint:

```text
POST /v1beta/extract
```

Focused excerpts from known URLs:

```json
{
  "urls": ["https://example.com"],
  "objective": "major changes in postgres 18",
  "excerpts": true,
  "full_content": false
}
```

Full-page crawl/markdown capture:

```json
{
  "urls": ["https://example.com"],
  "excerpts": true,
  "full_content": true
}
```

Typical response shape:

```json
{
  "extract_id": "extract_...",
  "results": [
    {
      "url": "https://example.com",
      "title": "Example",
      "publish_date": "2025-01-15",
      "excerpts": ["..."],
      "full_content": "# Example\\n..."
    }
  ],
  "errors": [],
  "warnings": null,
  "usage": [{"name": "sku_extract_excerpts", "count": 1}]
}
```

Use extract when:

- The user already gave a URL.
- You want markdown content from a page.
- You need to crawl a page, including JS-heavy pages or PDFs, instead of doing another search first.

## Good Defaults

- Start `search` with only `objective` unless you already know better query phrasing.
- Add `search_queries` when the search objective is broad or you want stronger recall.
- Use `extract --full-content` when you need the full page body.
- Leave `--full-content` off when excerpts are enough and you want a smaller response.
- Use `--payload` or `--payload-file` for advanced request fields instead of inventing new CLI flags.

## Official References

- Search quickstart: `https://docs.parallel.ai/search/search-quickstart.md`
- Extract quickstart: `https://docs.parallel.ai/extract/extract-quickstart.md`
- Public OpenAPI: `https://docs.parallel.ai/public-openapi.json`
