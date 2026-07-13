#!/usr/bin/env python3
import os
import urllib3
from openai import OpenAI

urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

GATEWAY_BASE_URL = "https://ai.gateway/openai/v1"

OPENAI_API_KEY = os.getenv("OPENAI_API_KEY", "your-internal-ai-local-key-here")


client = OpenAI(
    base_url=GATEWAY_BASE_URL,
    api_key=OPENAI_API_KEY,
    http_client=OpenAI()._client.__class__(verify=False),
)

print("==> Dispatching chat completion payload via secure gateway tunnel...")
try:
    response = client.chat.completions.create(
        model="gpt-4o-mini", messages=[{"role": "user", "content": "Hello"}]
    )

    print(response.choices[0].message.content)

except Exception as e:
    print(f"Gateway Routing Failed: {e}")
