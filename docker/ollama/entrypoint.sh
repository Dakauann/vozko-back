#!/bin/bash
set -e

# Default model to pull (can be overridden via OLLAMA_MODEL env var)
MODEL="${OLLAMA_MODEL:-all-minilm}"

echo "Starting Ollama server..."
ollama serve &
SERVER_PID=$!

# Wait for Ollama to be ready
echo "Waiting for Ollama to be ready..."
for i in {1..30}; do
    if ollama list > /dev/null 2>&1; then
        echo "Ollama is ready!"
        break
    fi
    echo "Waiting... ($i/30)"
    sleep 2
done

# Check if model exists, pull only if missing
echo "Checking for model: $MODEL"
if ollama list | grep -q "^$MODEL"; then
    echo "Model '$MODEL' already exists, skipping pull"
else
    echo "Model '$MODEL' not found, pulling..."
    ollama pull "$MODEL"
    echo "Model '$MODEL' pulled successfully"
fi

# Keep the server running
wait $SERVER_PID
