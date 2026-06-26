# Ready-to-Run Examples Guide

This guide demonstrates how to light up the `ai.local` data plane using the pre-configured environments and validate the proxy pipeline with different AI providers.

---

## 🎯 Case 1: Launching OpenAI Testing Suite

Navigate into the OpenAI test harness and light up the gateway:

```bash
# Spin up the data plane using the pre-configured OpenAI environment
sudo ai.local -d examples/openai/ -proxy-addr :443 

# Add API key
ai.local.cli key add /openai

# Open a separate terminal, set your internal client token, and fire away!
cd examples/openai/
export AI_LOCAL_KEY="sk-local-your-generated-key"

# Run standard non-streaming evaluation
./usage.py

# Run streaming evaluation
./stream.py
```

---

## 🎯 Case 2: Launching OpenRouter Testing Suite

If you are multiplexing downstream traffic through OpenRouter:

```bash
# Energize the engine utilizing the OpenRouter trajectory configuration
sudo ai.local -d examples/openrouter/ -proxy-addr :443 &

# Add API key
ai.local.cli key add /openrouter0
ai.local.cli key add /openrouter100

# Execute pre-configured bash automation test routes
cd examples/openrouter/


# Run standard non-streaming evaluation
OPENROUTER_API_KEY="sk-local-openrouter100-generated-key" ./usage.sh

# Run streaming evaluation
OPENROUTER_API_KEY="sk-local-openrouter0-generated-key" ./stream.sh
```
