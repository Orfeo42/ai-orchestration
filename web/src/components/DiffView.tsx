import { useEffect, useMemo, useState } from "react";
import {
  ChevronDown,
  ChevronRight,
  FileDiff as FileIcon,
  FilePlus,
  FileMinus,
  FilePen,
} from "lucide-react";
import { getRunDiff } from "@/api";
import type { FileDiff, FileStatus } from "@/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Tabs,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import { cn } from "@/lib/utils";

interface Props {
  runId: string;
  prNumber?: number;
}

type ViewMode = "unified" | "split";

const statusIcon: Record<FileStatus, typeof FileIcon> = {
  added: FilePlus,
  deleted: FileMinus,
  modified: FilePen,
  renamed: FileIcon,
};

const statusColor: Record<FileStatus, string> = {
  added: "text-emerald-500",
  deleted: "text-destructive",
  modified: "text-sky-500",
  renamed: "text-amber-500",
};

export default function DiffView({ runId, prNumber }: Props) {
  const [files, setFiles] = useState<FileDiff[] | null>(null);
  const [pr, setPR] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [view, setView] = useState<ViewMode>("unified");
  const [openMap, setOpenMap] = useState<Record<string, boolean>>({});

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    getRunDiff(runId)
      .then((res) => {
        if (cancelled) return;
        setFiles(res.files);
        setPR(res.pr_number);
        const init: Record<string, boolean> = {};
        for (const f of res.files) init[f.path] = true;
        setOpenMap(init);
      })
      .catch((e) => {
        if (cancelled) return;
        setError(String(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [runId, prNumber]);

  const allOpen = useMemo(() => {
    if (!files || files.length === 0) return false;
    return files.every((f) => openMap[f.path]);
  }, [files, openMap]);

  function toggleAll() {
    if (!files) return;
    const next: Record<string, boolean> = {};
    for (const f of files) next[f.path] = !allOpen;
    setOpenMap(next);
  }

  function setOpen(path: string, open: boolean) {
    setOpenMap((m) => ({ ...m, [path]: open }));
  }

  if (loading && !files) {
    return (
      <div className="rounded-md border border-dashed py-10 text-center text-sm text-muted-foreground">
        Loading diff...
      </div>
    );
  }
  if (error) {
    return (
      <div className="rounded-md border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
        {error}
      </div>
    );
  }
  if (!files || files.length === 0) {
    return (
      <div className="rounded-md border border-dashed py-10 text-center text-sm text-muted-foreground">
        {pr === 0
          ? "No PR associated with this run yet."
          : "No file changes."}
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-muted-foreground">
          {files.length} file{files.length === 1 ? "" : "s"} changed
          {pr ? ` · PR #${pr}` : ""}
        </span>
        <div className="ml-auto flex items-center gap-2">
          <Tabs value={view} onValueChange={(v) => setView(v as ViewMode)}>
            <TabsList className="h-8">
              <TabsTrigger value="unified" className="text-xs">
                Unified
              </TabsTrigger>
              <TabsTrigger value="split" className="text-xs">
                Split
              </TabsTrigger>
            </TabsList>
          </Tabs>
          <Button
            variant="outline"
            size="sm"
            onClick={toggleAll}
            className="h-8"
          >
            {allOpen ? (
              <>
                <ChevronRight className="h-3.5 w-3.5" />
                Collapse all
              </>
            ) : (
              <>
                <ChevronDown className="h-3.5 w-3.5" />
                Expand all
              </>
            )}
          </Button>
        </div>
      </div>
      {files.map((f) => (
        <FileBlock
          key={f.path}
          file={f}
          view={view}
          open={openMap[f.path] ?? true}
          onToggle={() => setOpen(f.path, !(openMap[f.path] ?? true))}
        />
      ))}
    </div>
  );
}

interface FileBlockProps {
  file: FileDiff;
  view: ViewMode;
  open: boolean;
  onToggle: () => void;
}

function FileBlock({ file, view, open, onToggle }: FileBlockProps) {
  const Icon = statusIcon[file.status] ?? FileIcon;
  return (
    <div className="overflow-hidden rounded-md border">
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-center gap-2 bg-muted/50 px-3 py-2 text-left text-sm hover:bg-muted"
      >
        {open ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        <Icon className={cn("h-4 w-4 shrink-0", statusColor[file.status])} />
        <span className="truncate font-mono text-xs">
          {file.old_path && file.old_path !== file.path
            ? `${file.old_path} → ${file.path}`
            : file.path}
        </span>
        <Badge variant="outline" className="ml-auto shrink-0 text-[10px]">
          {file.status}
        </Badge>
        <span className="shrink-0 font-mono text-xs text-emerald-500">
          +{file.additions}
        </span>
        <span className="shrink-0 font-mono text-xs text-destructive">
          -{file.deletions}
        </span>
      </button>
      {open && (
        <div className="bg-background font-mono text-xs leading-5">
          {view === "unified"
            ? renderUnified(file.patch)
            : renderSplit(file.patch)}
        </div>
      )}
    </div>
  );
}

function renderUnified(patch: string) {
  const lines = patch.split("\n");
  return lines.map((line, i) => {
    let cls = "";
    if (line.startsWith("+++") || line.startsWith("---")) {
      cls = "text-muted-foreground";
    } else if (line.startsWith("@@")) {
      cls = "bg-sky-500/10 text-sky-500";
    } else if (line.startsWith("diff --git") || line.startsWith("index ")) {
      cls = "text-muted-foreground";
    } else if (line.startsWith("+")) {
      cls = "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400";
    } else if (line.startsWith("-")) {
      cls = "bg-destructive/10 text-destructive";
    }
    return (
      <div
        key={i}
        className={cn("whitespace-pre-wrap break-all px-3", cls)}
      >
        {line || " "}
      </div>
    );
  });
}

interface SplitRow {
  kind: "context" | "change" | "hunk" | "meta";
  oldNum?: number;
  newNum?: number;
  oldText?: string;
  newText?: string;
  hunkText?: string;
  metaText?: string;
}

function renderSplit(patch: string) {
  const rows = buildSplitRows(patch);
  return (
    <table className="w-full table-fixed border-collapse">
      <colgroup>
        <col className="w-10" />
        <col />
        <col className="w-10" />
        <col />
      </colgroup>
      <tbody>
        {rows.map((row, i) => {
          if (row.kind === "meta") {
            return (
              <tr key={i}>
                <td
                  colSpan={4}
                  className="whitespace-pre-wrap break-all px-3 text-muted-foreground"
                >
                  {row.metaText || " "}
                </td>
              </tr>
            );
          }
          if (row.kind === "hunk") {
            return (
              <tr key={i} className="bg-sky-500/10 text-sky-500">
                <td
                  colSpan={4}
                  className="whitespace-pre-wrap break-all px-3"
                >
                  {row.hunkText}
                </td>
              </tr>
            );
          }
          const leftCls =
            row.kind === "change" && row.oldText !== undefined
              ? "bg-destructive/10 text-destructive"
              : row.oldText === undefined
                ? "bg-muted/30"
                : "";
          const rightCls =
            row.kind === "change" && row.newText !== undefined
              ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
              : row.newText === undefined
                ? "bg-muted/30"
                : "";
          return (
            <tr key={i} className="align-top">
              <td className="select-none border-r px-2 text-right text-muted-foreground">
                {row.oldNum ?? ""}
              </td>
              <td
                className={cn(
                  "whitespace-pre-wrap break-all px-3",
                  leftCls,
                )}
              >
                {row.oldText ?? " "}
              </td>
              <td className="select-none border-l border-r px-2 text-right text-muted-foreground">
                {row.newNum ?? ""}
              </td>
              <td
                className={cn(
                  "whitespace-pre-wrap break-all px-3",
                  rightCls,
                )}
              >
                {row.newText ?? " "}
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

function buildSplitRows(patch: string): SplitRow[] {
  const lines = patch.split("\n");
  const rows: SplitRow[] = [];
  let oldNum = 0;
  let newNum = 0;
  let dels: { num: number; text: string }[] = [];
  let adds: { num: number; text: string }[] = [];

  function flushChanges() {
    const max = Math.max(dels.length, adds.length);
    for (let i = 0; i < max; i++) {
      rows.push({
        kind: "change",
        oldNum: dels[i]?.num,
        oldText: dels[i]?.text,
        newNum: adds[i]?.num,
        newText: adds[i]?.text,
      });
    }
    dels = [];
    adds = [];
  }

  for (const line of lines) {
    if (
      line.startsWith("diff --git") ||
      line.startsWith("index ") ||
      line.startsWith("+++") ||
      line.startsWith("---") ||
      line.startsWith("new file") ||
      line.startsWith("deleted file") ||
      line.startsWith("rename ") ||
      line.startsWith("similarity ") ||
      line.startsWith("Binary ")
    ) {
      flushChanges();
      rows.push({ kind: "meta", metaText: line });
      continue;
    }
    if (line.startsWith("@@")) {
      flushChanges();
      const m = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(line);
      if (m) {
        oldNum = parseInt(m[1], 10);
        newNum = parseInt(m[2], 10);
      }
      rows.push({ kind: "hunk", hunkText: line });
      continue;
    }
    if (line.startsWith("-")) {
      dels.push({ num: oldNum, text: line.slice(1) });
      oldNum++;
      continue;
    }
    if (line.startsWith("+")) {
      adds.push({ num: newNum, text: line.slice(1) });
      newNum++;
      continue;
    }
    flushChanges();
    const text = line.startsWith(" ") ? line.slice(1) : line;
    rows.push({
      kind: "context",
      oldNum,
      newNum,
      oldText: text,
      newText: text,
    });
    oldNum++;
    newNum++;
  }
  flushChanges();
  return rows;
}
