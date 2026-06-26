#!/usr/bin/env python3
import os
import urllib3
from openai import OpenAI

urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

GATEWAY_BASE_URL = "https://ai.local/openai/v1"

INTERNAL_AI_LOCAL_KEY = os.getenv("AI_LOCAL_KEY", "your-internal-ai-local-key-here")

client = OpenAI(
    base_url=GATEWAY_BASE_URL,
    api_key=INTERNAL_AI_LOCAL_KEY,
    http_client=OpenAI()._client.__class__(verify=False),
)

print("==> Dispatching streaming chat completion payload (max_tokens=1000)...")
try:
    response_stream = client.chat.completions.create(
        model="gpt-4o-mini",
        max_tokens=1000,
        messages=[{"role": "user", "content": "Who is best cyclist in the world"}],
        stream=True,
    )

    print("\n✨ Streaming Response From Gateway:")
    for chunk in response_stream:
        if chunk.choices and chunk.choices[0].delta.content is not None:
            print(chunk.choices[0].delta.content, end="", flush=True)
    print("\n\nStream Pipeline Successfully Completed.")

except Exception as e:
    print(f"\nGateway Streaming Failed: {e}")
