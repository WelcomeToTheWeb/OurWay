---
name: SearXNG Web Search
description: Search the web using the local SearXNG instance at http://localhost:8080. Use when you need current information, web search, or to look up URLs.
---

## Web Search with SearXNG

Search the web using the local SearXNG instance. Returns JSON results with URLs, titles, and content snippets.

### Basic Search

```bash
curl -s "http://localhost:8080/search?q=YOUR+QUERY+HERE&format=json" | jq '.results[] | {url, title, content}'
```

### Search Examples

Search for information about a topic:
```bash
curl -s "http://localhost:8080/search?q=TypeScript+best+practices&format=json" | jq '.results[] | {url, title}'
```

Limit results to a specific number:
```bash
curl -s "http://localhost:8080/search?q=React+hooks&format=json&limit=3" | jq '.results[] | {url, title}'
```

Search a specific category (e.g., news, images, videos):
```bash
curl -s "http://localhost:8080/search?q=AI+news&format=json&categories=news" | jq '.results[] | {url, title}'
```

### Response Format

The API returns JSON with these fields per result:
- `url`: The result URL
- `title`: The result title
- `content`: A text snippet
- `engine`: The search engine that found it
- `publishedDate`: Date if available

### When to Use

- Look up current information or recent events
- Find documentation or references for a technology
- Verify facts or get multiple perspectives
- Search for specific URLs or resources online

### Tips

- Use `jq` to parse and format the JSON output
- Multiple words are searched as a phrase by default
- Use quotes for exact phrase matching: `q="exact phrase"`
- If results are too narrow, remove some keywords