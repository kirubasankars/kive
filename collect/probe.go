// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package collect

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"kive/workspace"
)

const probeScript = `set -eu
# MemTotal is already total RAM; round up to whole GiB so identical boxes report stable capacity.
mem_kb=$(awk '/MemTotal/ {print $2; exit}' /proc/meminfo)
mem_mb=$(( (mem_kb + 1023) / 1024 ))
mem_mb=$(( ((mem_mb + 1023) / 1024) * 1024 ))
cpus=$(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN)
# Prefer max frequency (capacity), not current /proc/cpuinfo "cpu MHz" (scales with load).
mhz=""
for f in /sys/devices/system/cpu/cpu*/cpufreq/cpuinfo_max_freq; do
  if [ -r "$f" ]; then
    khz=$(cat "$f")
    if [ -n "${khz}" ] && [ "${khz}" -gt 0 ] 2>/dev/null; then
      mhz=$(( khz / 1000 ))
      break
    fi
  fi
done
if [ -z "${mhz}" ] || [ "${mhz}" = 0 ]; then
  if command -v lscpu >/dev/null 2>&1; then
    mhz=$(lscpu 2>/dev/null | awk -F: '/CPU max MHz/{gsub(/^[ \t]+/,"",$2); print int($2); exit}')
  fi
fi
if [ -z "${mhz}" ] || [ "${mhz}" = 0 ]; then
  mhz=$(awk -F: '/cpu MHz/{gsub(/^[ \t]+/,"",$2); print int($2); exit}' /proc/cpuinfo)
fi
if [ -z "${mhz}" ] || [ "${mhz}" = 0 ]; then
  echo "CPU_MHZ=0" >&2
  exit 1
fi
cpu_mhz=$(( cpus * mhz ))
printf 'MEMORY_MB=%s\nCPU_MHZ=%s\n' "${mem_mb}" "${cpu_mhz}"

volume_idx=0
if command -v lsblk >/dev/null 2>&1 && command -v df >/dev/null 2>&1; then
  while read -r device size type; do
    [ "${type}" = "disk" ] || continue
    base="${device##*/}"
    case "${base}" in
      zram*|loop*|ram*|fd*) continue ;;
    esac
    used_b=0
    seen_mounts=""
    while read -r mount; do
      mount="${mount#"${mount%%[![:space:]]*}"}"
      mount="${mount%"${mount##*[![:space:]]}"}"
      [ -n "${mount}" ] || continue
      case " ${seen_mounts} " in
        *" ${mount} "*) continue ;;
      esac
      seen_mounts="${seen_mounts} ${mount}"
      used=$(df -B1 --output=used "${mount}" 2>/dev/null | tail -1)
      [ -n "${used}" ] || continue
      used_b=$(( used_b + used ))
    done < <(lsblk -ln -o MOUNTPOINT "${device}" | sed '/^[[:space:]]*$/d' | sort -u)
    # Device capacity: round up to whole SI GB (marketing size), store as N*1024 MiB so display is "N gb".
    size_gb_si=$(( (size + 999999999) / 1000000000 ))
    if [ "${size_gb_si}" -lt 1 ]; then
      size_gb_si=1
    fi
    size_mb=$(( size_gb_si * 1024 ))
    used_mb=$(( (used_b + 1048575) / 1048576 ))
    if [ "${size}" -gt 0 ]; then
      usage_pct=$(( (used_b * 100 + size / 2) / size ))
    else
      usage_pct=0
    fi
    printf 'VOLUME_%d_DEVICE=%s\n' "${volume_idx}" "${device}"
    printf 'VOLUME_%d_SIZE_MB=%s\n' "${volume_idx}" "${size_mb}"
    printf 'VOLUME_%d_USAGE_MB=%s\n' "${volume_idx}" "${used_mb}"
    printf 'VOLUME_%d_USAGE_PCT=%s\n' "${volume_idx}" "${usage_pct}"
    volume_idx=$(( volume_idx + 1 ))
  done < <(lsblk -bdpo NAME,SIZE,TYPE)
fi
printf 'VOLUME_COUNT=%s\n' "${volume_idx}"
`

type volumeFacts struct {
	Device   string
	SizeMB   float64
	UsageMB  float64
	UsagePct float64
}

type factsProbeResult struct {
	MemoryCPU workspace.WorkerFacts
	Volumes   []volumeFacts
}

type probeOutput struct {
	MemoryMB float64
	CPUMHz   float64
	Volumes  []volumeFacts
}

func parseProbeOutput(output string) (probeOutput, error) {
	values := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.ToUpper(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}

	memoryRaw, ok := values["MEMORY_MB"]
	if !ok || memoryRaw == "" {
		return probeOutput{}, fmt.Errorf("probe output missing MEMORY_MB")
	}
	memoryMB, err := strconv.ParseFloat(memoryRaw, 64)
	if err != nil {
		return probeOutput{}, fmt.Errorf("parse MEMORY_MB %q: %w", memoryRaw, err)
	}
	if memoryMB <= 0 {
		return probeOutput{}, fmt.Errorf("probe reported invalid memory %s", memoryRaw)
	}

	cpuRaw, ok := values["CPU_MHZ"]
	if !ok || cpuRaw == "" {
		return probeOutput{}, fmt.Errorf("probe output missing CPU_MHZ")
	}
	cpuMHz, err := strconv.ParseFloat(cpuRaw, 64)
	if err != nil {
		return probeOutput{}, fmt.Errorf("parse CPU_MHZ %q: %w", cpuRaw, err)
	}
	if cpuMHz <= 0 {
		return probeOutput{}, fmt.Errorf("probe reported invalid cpu %s", cpuRaw)
	}

	volumes, err := parseVolumeFacts(values)
	if err != nil {
		return probeOutput{}, err
	}

	return probeOutput{
		MemoryMB: memoryMB,
		CPUMHz:   cpuMHz,
		Volumes:  volumes,
	}, nil
}

func parseVolumeFacts(values map[string]string) ([]volumeFacts, error) {
	countRaw, ok := values["VOLUME_COUNT"]
	if !ok || countRaw == "" {
		return nil, nil
	}
	volumeCount, err := strconv.Atoi(countRaw)
	if err != nil {
		return nil, fmt.Errorf("parse VOLUME_COUNT %q: %w", countRaw, err)
	}
	if volumeCount < 0 {
		return nil, fmt.Errorf("probe reported invalid volume count %s", countRaw)
	}

	volumes := make([]volumeFacts, 0, volumeCount)
	for idx := range volumeCount {
		prefix := fmt.Sprintf("VOLUME_%d_", idx)

		device, ok := values[prefix+"DEVICE"]
		if !ok || device == "" {
			return nil, fmt.Errorf("probe output missing %sDEVICE", prefix)
		}

		sizeMB, err := parseVolumeField(values, prefix+"SIZE_MB", "size")
		if err != nil {
			return nil, err
		}
		usageMB, err := parseVolumeField(values, prefix+"USAGE_MB", "usage")
		if err != nil {
			return nil, err
		}
		usagePct, err := parseVolumeField(values, prefix+"USAGE_PCT", "usage_pct")
		if err != nil {
			return nil, err
		}

		volumes = append(volumes, volumeFacts{
			Device:   device,
			SizeMB:   sizeMB,
			UsageMB:  usageMB,
			UsagePct: usagePct,
		})
	}
	return volumes, nil
}

func parseVolumeField(values map[string]string, key, label string) (float64, error) {
	raw, ok := values[key]
	if !ok || raw == "" {
		return 0, fmt.Errorf("probe output missing %s", key)
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s %q: %w", label, raw, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("probe reported invalid %s %s", label, raw)
	}
	return value, nil
}

func toFactsProbeResult(probe probeOutput) factsProbeResult {
	return factsProbeResult{
		MemoryCPU: workspace.WorkerFacts{
			MemoryMB: probe.MemoryMB,
			CPUMHz:   probe.CPUMHz,
		},
		Volumes: probe.Volumes,
	}
}

func formatDiskSizeMB(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%d gb", int(math.Round(mb/1024)))
	}
	return fmt.Sprintf("%d mb", int(math.Round(mb)))
}

func formatUsagePct(pct float64) string {
	return fmt.Sprintf("%d%%", int(math.Round(pct)))
}

func formatVolumeField(volume volumeFacts) string {
	return fmt.Sprintf(
		`%s size=%s usage=%s usage_pct=%s`,
		volume.Device,
		formatDiskSizeMB(volume.SizeMB),
		formatDiskSizeMB(volume.UsageMB),
		formatUsagePct(volume.UsagePct),
	)
}
