import { Plus, Trash2 } from "lucide-react";
import { FilterSelect } from "@/components/FilterSelect";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  newMatchRow,
  type EditorState,
  type TriggerKind,
} from "@/lib/workflowEditor";
import type { CatalogTrigger } from "@/types";

type Props = {
  state: EditorState;
  triggers: readonly CatalogTrigger[];
  onChange: (patch: Partial<EditorState>) => void;
};

const kinds: readonly TriggerKind[] = ["none", "plugin", "forgejo"];

export function TriggerEditor({ state, triggers, onChange }: Props) {
  const options = triggers.map((t) => {
    const value = `${t.plugin}/${t.name}`;
    return { value, label: value };
  });
  if (
    state.pluginTrigger !== "" &&
    !options.some((o) => o.value === state.pluginTrigger)
  ) {
    options.push({ value: state.pluginTrigger, label: state.pluginTrigger });
  }

  function updateRow(key: string, field: "name" | "value", text: string) {
    onChange({
      matchRows: state.matchRows.map((r) =>
        r.key === key ? { ...r, [field]: text } : r,
      ),
    });
  }

  return (
    <fieldset className="grid gap-3">
      <legend className="mb-2 text-sm font-medium">Trigger</legend>
      <div className="flex gap-4">
        {kinds.map((kind) => (
          <label key={kind} className="flex items-center gap-2 text-sm">
            <input
              type="radio"
              name="trigger-kind"
              value={kind}
              checked={state.triggerKind === kind}
              onChange={() => {
                onChange({ triggerKind: kind });
              }}
            />
            {kind}
          </label>
        ))}
      </div>

      {state.triggerKind === "plugin" && (
        <div className="grid gap-3">
          <div className="grid gap-2">
            <Label htmlFor="trigger-plugin">Plugin trigger</Label>
            <FilterSelect
              id="trigger-plugin"
              label="Plugin trigger"
              value={state.pluginTrigger}
              options={options}
              onChange={(value) => {
                onChange({ pluginTrigger: value });
              }}
            />
          </div>
          <div className="grid gap-2">
            <span className="text-sm font-medium">Match</span>
            {state.matchRows.map((row, i) => (
              <div key={row.key} className="flex items-center gap-2">
                <Input
                  aria-label={`Match key ${String(i + 1)}`}
                  value={row.name}
                  placeholder="key"
                  onChange={(e) => {
                    updateRow(row.key, "name", e.target.value);
                  }}
                />
                <Input
                  aria-label={`Match value ${String(i + 1)}`}
                  value={row.value}
                  placeholder="value"
                  onChange={(e) => {
                    updateRow(row.key, "value", e.target.value);
                  }}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={`Remove match ${String(i + 1)}`}
                  onClick={() => {
                    onChange({
                      matchRows: state.matchRows.filter(
                        (r) => r.key !== row.key,
                      ),
                    });
                  }}
                >
                  <Trash2 />
                </Button>
              </div>
            ))}
            <div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  onChange({ matchRows: [...state.matchRows, newMatchRow()] });
                }}
              >
                <Plus />
                Add match
              </Button>
            </div>
          </div>
        </div>
      )}

      {state.triggerKind === "forgejo" && (
        <div className="grid gap-3">
          <div className="grid gap-2">
            <Label htmlFor="forgejo-repos">Repos (comma separated)</Label>
            <Input
              id="forgejo-repos"
              value={state.forgejoRepos}
              placeholder="owner/repo, owner/other"
              onChange={(e) => {
                onChange({ forgejoRepos: e.target.value });
              }}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="forgejo-label">Label</Label>
            <Input
              id="forgejo-label"
              value={state.forgejoLabel}
              onChange={(e) => {
                onChange({ forgejoLabel: e.target.value });
              }}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="forgejo-assignee">Assignee</Label>
            <Input
              id="forgejo-assignee"
              value={state.forgejoAssignee}
              onChange={(e) => {
                onChange({ forgejoAssignee: e.target.value });
              }}
            />
          </div>
        </div>
      )}
    </fieldset>
  );
}
