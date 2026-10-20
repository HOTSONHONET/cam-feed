#!/usr/bin/env bash
set -euo pipefail

VER="1.24.2"
ROOT="third_party/onnxruntime/${VER}"
mkdir -p "${ROOT}"
cd "${ROOT}"

CPU_URL="https://github.com/microsoft/onnxruntime/releases/download/v${VER}/onnxruntime-linux-x64-${VER}.tgz"
GPU_URL="https://github.com/microsoft/onnxruntime/releases/download/v${VER}/onnxruntime-linux-x64-gpu-${VER}.tgz"

echo "[+] Download CPU"
wget -O ort_cpu.tgz "${CPU_URL}"
tar -xzf ort_cpu.tgz

echo "[+] Download GPU"
wget -O ort_gpu.tgz "${GPU_URL}"
tar -xzf ort_gpu.tgz

CPU_DIR="onnxruntime-linux-x64-${VER}"
GPU_DIR="onnxruntime-linux-x64-gpu-${VER}"

# Copy the main runtime library.
# Prefer the unversioned lib if present, else fallback to first match.
CPU_LIB="$(ls -1 ${CPU_DIR}/lib/libonnxruntime.so 2>/dev/null || true)"
if [[ -z "${CPU_LIB}" ]]; then
  CPU_LIB="$(ls -1 ${CPU_DIR}/lib/libonnxruntime.so* | head -n 1)"
fi

GPU_LIB="$(ls -1 ${GPU_DIR}/lib/libonnxruntime.so 2>/dev/null || true)"
if [[ -z "${GPU_LIB}" ]]; then
  GPU_LIB="$(ls -1 ${GPU_DIR}/lib/libonnxruntime.so* | head -n 1)"
fi

cp "${CPU_LIB}" ./onnxruntime_cpu.so
cp "${GPU_LIB}" ./onnxruntime_gpu.so

# Copy GPU provider libs (keep near the gpu .so for easier LD_LIBRARY_PATH)
mkdir -p gpu_libs
cp ${GPU_DIR}/lib/*.so* gpu_libs/ || true

echo "[✓] Done."
echo "CPU: ${ROOT}/onnxruntime_cpu.so"
echo "GPU: ${ROOT}/onnxruntime_gpu.so"
echo "GPU libs: ${ROOT}/gpu_libs/"