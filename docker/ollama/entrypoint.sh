#!/bin/bash
set -e

# Default model to pull (can be overridden via OLLAMA_MODEL env var)
MODEL="${OLLAMA_MODEL:-all-minilm}"

# Threads llama.cpp should use. It must match the number of CPUs this container
# is actually allowed to run on.
#
# llama.cpp sizes its own thread pool from the HOST cpu count: it does not read
# the cgroup cpuset, so under `cpuset: 0-5,12-17` it still detected 24 and
# started 24 threads on 12 cores. The 2x oversubscription turned llama.cpp's
# spin-wait barriers between layers into scheduler stalls and cost ~10x: a
# one-sentence query took ~5s instead of ~0.5s, and a 10-chunk indexing batch
# took ~45s instead of ~4.7s, close enough to the Go client's 60s timeout to
# fail under any extra load. Nothing in the API surfaced this; it looks purely
# like "CPU embedding is slow".
#
# Keep this equal to cpuset width / OLLAMA_NUM_PARALLEL, so that all slots busy
# adds up to the cpuset exactly and never oversubscribes it.
NUM_THREAD="${OLLAMA_NUM_THREAD:-12}"

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

# Bake num_thread into the model under its OWN name.
#
# The Go embedding client requests the bare model name and never sends
# `options`, so a tuned copy under a different tag would simply never be used,
# and pointing the app at a new name would mean editing DefaultEmbeddingModel
# plus every knowledge base's stored config. Re-creating the tag in place keeps
# "bge-m3" meaning "bge-m3 configured for this host". Blobs are content
# addressed and shared, so this rewrites the manifest without re-downloading the
# 1.2 GB of weights, and it is idempotent across restarts.
echo "Applying num_thread=$NUM_THREAD to '$MODEL'..."
MODELFILE="$(mktemp)"
printf 'FROM %s\nPARAMETER num_thread %s\n' "$MODEL" "$NUM_THREAD" > "$MODELFILE"
if ollama create "$MODEL" -f "$MODELFILE"; then
    echo "Model '$MODEL' now pinned to $NUM_THREAD threads"
else
    echo "WARNING: could not apply num_thread to '$MODEL'; it will run with llama.cpp's own (host-derived) thread count and be several times slower"
fi
rm -f "$MODELFILE"

# Keep the server running
wait $SERVER_PID
