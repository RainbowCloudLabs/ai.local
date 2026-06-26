#!/bin/sh
curl -k https://ai.local/openrouter0/api/v1/chat/completions \
  -H "Authorization: Bearer $OPENROUTER_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "google/gemini-2.5-flash",
    "max_tokens": 2000,
    "stream": true,
    "stream_options": {
      "include_usage": true
    },
    "messages": [
      {"role": "user", "content": "who is MVP of NBA 2026 final?"}
    ]
  }'
