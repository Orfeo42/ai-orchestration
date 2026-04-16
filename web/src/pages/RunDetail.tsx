import { useState } from "react";
import { useParams } from "react-router-dom";
import EventLog from "../components/EventLog";
import { cancelRun, injectNote } from "../api";
import { useRunStream } from "../ws";

export default function RunDetail() {
  const { id } = useParams();
  const { run, events, connected, error } = useRunStream(id);
  const [note, setNote] = useState("");
  const [actionMsg, setActionMsg] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  if (!id) return <div>Missing run id.</div>;

  const totalIn = run?.usage.input ?? 0;
  const totalOut = run?.usage.output ?? 0;
  const totalCacheRead = run?.usage.cache_read_input ?? 0;
  const totalCacheCreate = run?.usage.cache_creation_input ?? 0;
  const terminal =
    run?.phase === "done" || run?.phase === "failed" || run?.phase === "cancelled";

  async function onCancel() {
    if (!id) return;
    setBusy(true);
    try {
      await cancelRun(id);
      setActionMsg("cancel sent");
    } catch (e) {
      setActionMsg(String(e));
    } finally {
      setBusy(false);
    }
  }

  async function onInject() {
    if (!id || !note.trim()) return;
    setBusy(true);
    try {
      await injectNote(id, note.trim());
      setActionMsg("note sent");
      setNote("");
    } catch (e) {
      setActionMsg(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="run-detail">
      <header className="page-header">
        <div>
          <h2>{run?.title ?? id}</h2>
          <div className="meta">
            {run && (
              <>
                <span>{run.service}</span>
                <span>
                  {run.repo_owner}/{run.repo_name}
                </span>
                {run.issue_number ? <span>#{run.issue_number}</span> : null}
                {run.pr_number ? <span>PR#{run.pr_number}</span> : null}
                <span className={`phase-badge phase-${run.phase}`}>{run.phase}</span>
                <span>attempt {run.attempt}</span>
              </>
            )}
          </div>
        </div>
        <div className="actions">
          <span className={connected ? "ws-on" : "ws-off"}>{connected ? "live" : "offline"}</span>
          <button onClick={onCancel} disabled={busy || terminal}>
            Stop
          </button>
        </div>
      </header>

      {error && <div className="error">{error}</div>}

      {run && (
        <div className="usage-bar">
          <div>input: {totalIn}</div>
          <div>output: {totalOut}</div>
          <div>cache read: {totalCacheRead}</div>
          <div>cache create: {totalCacheCreate}</div>
        </div>
      )}

      <div className="inject-form">
        <textarea
          rows={2}
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="Add instructions to inject (will pause Claude and resume with this note)..."
          disabled={terminal}
        />
        <button onClick={onInject} disabled={busy || terminal || !note.trim()}>
          Inject
        </button>
      </div>
      {actionMsg && <div className="action-msg">{actionMsg}</div>}

      <EventLog events={events} />
    </section>
  );
}
