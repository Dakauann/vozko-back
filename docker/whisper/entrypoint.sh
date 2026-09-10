#!/bin/sh
set -e

MODEL_DIR="${MODEL_DIR:-/models}"
MODEL_NAME="${MODEL_NAME:-ggml-small.bin}"
MODEL_PATH="${MODEL_DIR}/${MODEL_NAME}"

mkdir -p "$MODEL_DIR"

# Fetch once, into the volume. Downloading to a temporary name and renaming on
# success is what keeps a container killed mid-download from leaving a truncated
# file that whisper-server would then load and fail on for every request.
if [ ! -s "$MODEL_PATH" ]; then
    echo "[whisper] model $MODEL_NAME not present, downloading..."
    curl -fL --retry 3 --retry-delay 5 \
        "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/${MODEL_NAME}" \
        -o "${MODEL_PATH}.part"
    mv "${MODEL_PATH}.part" "$MODEL_PATH"
    echo "[whisper] model downloaded: $(du -h "$MODEL_PATH" | cut -f1)"
else
    echo "[whisper] model already present: $MODEL_PATH ($(du -h "$MODEL_PATH" | cut -f1))"
fi

# THREADS must match the width of the container's cpuset, and it is passed in
# rather than derived.
#
# whisper.cpp sizes nothing from the cgroup: like llama.cpp it reads the HOST
# cpu count, so on a 24-core box inside a 4-core cpuset it would start 24
# threads on 4 cores. The oversubscription turns its spin-wait barriers into
# scheduler stalls, which is the same trap documented at length in
# docker/ollama/entrypoint.sh and measured there at roughly 10x.
echo "[whisper] starting server: threads=${THREADS} processors=${PROCESSORS} lang=${LANGUAGE} port=${PORT}"

exec whisper-server \
    -m "$MODEL_PATH" \
    -t "${THREADS}" \
    -p "${PROCESSORS}" \
    --host "${HOST}" \
    --port "${PORT}" \
    -l "${LANGUAGE}" \
    --convert
