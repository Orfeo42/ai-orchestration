import type { TokenUsage } from "@/types";

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${String(Math.round(ms))}ms`;
  const totalSeconds = Math.round(ms / 1000);
  if (totalSeconds < 60) return `${String(totalSeconds)}s`;
  const totalMinutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (totalMinutes < 60) return `${String(totalMinutes)}m ${String(seconds)}s`;
  const hours = Math.floor(totalMinutes / 60);
  return `${String(hours)}h ${String(totalMinutes % 60)}m`;
}

export function formatDurationBetween(
  start: string | undefined,
  end: string | undefined,
): string {
  if (start === undefined || end === undefined) return "-";
  const ms = new Date(end).getTime() - new Date(start).getTime();
  if (Number.isNaN(ms) || ms < 0) return "-";
  return formatDuration(ms);
}

export function formatDateTime(iso: string | undefined): string {
  if (iso === undefined) return "-";
  return new Date(iso).toLocaleString();
}

export function formatPercent(fraction: number): string {
  return `${(fraction * 100).toFixed(1)}%`;
}

export function formatCount(n: number): string {
  return n.toLocaleString("en-US");
}

export function formatTokenPair(usage: TokenUsage): string {
  return `${formatCount(usage.input)} / ${formatCount(usage.output)}`;
}

export function toISODate(d: Date): string {
  return d.toISOString().slice(0, 10);
}

export function lastDaysRange(days: number): { from: string; to: string } {
  const to = new Date();
  const from = new Date(to.getTime() - days * 24 * 60 * 60 * 1000);
  return { from: toISODate(from), to: toISODate(to) };
}
