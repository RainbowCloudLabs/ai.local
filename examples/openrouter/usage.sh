#!/bin/sh
curl -k https://ai.gateway/openrouter100/api/v1/chat/completions \
  -H "Authorization: Bearer $OPENROUTER_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "google/gemini-2.5-flash",
    "max_tokens": 5,
    "messages": [
      {"role": "user", "content": "hi"}
    ]
  }'
