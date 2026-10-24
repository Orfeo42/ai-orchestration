import { describe, expect, it } from "vitest";
import {
  formatDuration,
  formatDurationBetween,
  formatPercent,
  lastDaysRange,
} from "./format";

describe("formatDuration", () => {
  it("formats milliseconds", () => {
    expect(formatDuration(450)).toBe("450ms");
  });

  it("formats seconds", () => {
    expect(formatDuration(5000)).toBe("5s");
  });

  it("formats minutes and seconds", () => {
    expect(formatDuration(125000)).toBe("2m 5s");
  });

  it("formats hours and minutes", () => {
    expect(formatDuration(3_900_000)).toBe("1h 5m");
  });
});

describe("formatDurationBetween", () => {
  it("returns a dash when the end is missing", () => {
    expect(formatDurationBetween("2026-10-01T10:00:00Z", undefined)).toBe("-");
  });

  it("returns the elapsed time", () => {
    expect(
      formatDurationBetween("2026-10-01T10:00:00Z", "2026-10-01T10:02:00Z"),
    ).toBe("2m 0s");
  });
});

describe("formatPercent", () => {
  it("renders a fraction as a percentage", () => {
    expect(formatPercent(0.4567)).toBe("45.7%");
  });
});

describe("lastDaysRange", () => {
  it("returns ISO dates 30 days apart", () => {
    const { from, to } = lastDaysRange(30);

    expect(from).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(to).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    const days = (Date.parse(to) - Date.parse(from)) / 86_400_000;
    expect(days).toBe(30);
  });
});
