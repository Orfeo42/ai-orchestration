import { useCallback, useMemo, useState, type SubmitEvent } from "react";
import { useNavigate, useParams } from "react-router";
import { Plus } from "lucide-react";
import { ApiError } from "@/api";
import { FilterSelect } from "@/components/FilterSelect";
import { StepEditorCard } from "@/components/StepEditorCard";
import { TriggerEditor } from "@/components/TriggerEditor";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  addStep,
  emptyEditorState,
  fromDefinition,
  hasErrors,
  removeStep,
  setTransition,
  setUses,
  stepErrors,
  toDefinition,
  updateStep,
  type EditorState,
  type EditorStep,
} from "@/lib/workflowEditor";
import { useCatalog, useSaveWorkflow, useWorkflowVersions } from "@/queries";
import type { Catalog } from "@/types";

type FormProps = {
  initial: EditorState;
  catalog: Catalog;
};

function EditorForm({ initial, catalog }: FormProps) {
  const navigate = useNavigate();
  const save = useSaveWorkflow();
  const [state, setState] = useState(initial);
  const catalogSteps = catalog.steps;

  const patchState = useCallback((patch: Partial<EditorState>) => {
    setState((s) => ({ ...s, ...patch }));
  }, []);
  const handleUpdate = useCallback(
    (key: string, patch: Partial<EditorStep>) => {
      setState((s) => updateStep(s, key, patch));
    },
    [],
  );
  const handleUses = useCallback(
    (key: string, uses: string) => {
      const outcomes =
        catalogSteps.find((c) => c.name === uses)?.outcomes ?? [];
      setState((s) => setUses(s, key, uses, outcomes));
    },
    [catalogSteps],
  );
  const handleTransition = useCallback(
    (key: string, outcome: string, target: string) => {
      setState((s) => setTransition(s, key, outcome, target));
    },
    [],
  );
  const handleRemove = useCallback((key: string) => {
    setState((s) => removeStep(s, key));
  }, []);

  const startOptions = useMemo(
    () =>
      state.steps.map((s) => ({
        value: s.key,
        label: s.id.trim() === "" ? "(unnamed)" : s.id,
      })),
    [state.steps],
  );
  const invalid = hasErrors(state);
  const problems =
    save.error instanceof ApiError ? save.error.problems : undefined;

  function handleSubmit(e: SubmitEvent<HTMLFormElement>) {
    e.preventDefault();
    const definition = toDefinition(state);
    save.mutate(definition, {
      onSuccess: () => {
        void navigate(`/workflows/${encodeURIComponent(definition.name)}`);
      },
    });
  }

  return (
    <form onSubmit={handleSubmit} className="flex max-w-3xl flex-col gap-5">
      <h1 className="text-2xl font-semibold tracking-tight">
        {state.isNew ? "New workflow" : `Edit ${state.name}`}
      </h1>

      <Card>
        <CardContent className="grid gap-4 pt-6">
          <div className="grid gap-2">
            <Label htmlFor="wf-name">Name</Label>
            <Input
              id="wf-name"
              value={state.name}
              disabled={!state.isNew}
              onChange={(e) => {
                patchState({ name: e.target.value });
              }}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="wf-description">Description</Label>
            <Input
              id="wf-description"
              value={state.description}
              onChange={(e) => {
                patchState({ description: e.target.value });
              }}
            />
          </div>
          <TriggerEditor
            state={state}
            triggers={catalog.triggers}
            onChange={patchState}
          />
          <div className="grid gap-2">
            <Label htmlFor="wf-start">Start step</Label>
            <FilterSelect
              id="wf-start"
              label="Start step"
              value={state.start}
              options={startOptions}
              onChange={(start) => {
                patchState({ start });
              }}
            />
          </div>
        </CardContent>
      </Card>

      {state.steps.map((step) => (
        <StepEditorCard
          key={step.key}
          step={step}
          steps={state.steps}
          catalogSteps={catalogSteps}
          errors={stepErrors(state, step)}
          onUpdate={handleUpdate}
          onUses={handleUses}
          onTransition={handleTransition}
          onRemove={handleRemove}
        />
      ))}

      <div>
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            setState(addStep);
          }}
        >
          <Plus />
          Add step
        </Button>
      </div>

      {save.error && (
        <Alert variant="destructive">
          <AlertDescription>
            <p>{save.error.message}</p>
            {problems !== undefined && (
              <ul className="list-disc pl-5">
                {problems.map((p) => (
                  <li key={p}>{p}</li>
                ))}
              </ul>
            )}
          </AlertDescription>
        </Alert>
      )}

      <div>
        <Button type="submit" disabled={invalid || save.isPending}>
          Save
        </Button>
      </div>
    </form>
  );
}

type ScreenProps = { initial: EditorState };

function EditorScreen({ initial }: ScreenProps) {
  const catalog = useCatalog();
  if (catalog.isPending) {
    return <p className="text-sm text-muted-foreground">Loading catalog...</p>;
  }
  if (catalog.error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{catalog.error.message}</AlertDescription>
      </Alert>
    );
  }
  return <EditorForm initial={initial} catalog={catalog.data} />;
}

type EditProps = { name: string };

function EditWorkflow({ name }: EditProps) {
  const versions = useWorkflowVersions(name);
  const latest = useMemo(
    () => [...(versions.data ?? [])].sort((a, b) => b.id - a.id)[0],
    [versions.data],
  );
  const initial = useMemo(
    () => (latest ? fromDefinition(latest.definition) : undefined),
    [latest],
  );

  if (versions.isPending) {
    return <p className="text-sm text-muted-foreground">Loading workflow...</p>;
  }
  if (versions.error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{versions.error.message}</AlertDescription>
      </Alert>
    );
  }
  if (!initial) {
    return <p className="text-sm text-muted-foreground">No versions found.</p>;
  }
  return <EditorScreen initial={initial} />;
}

export function WorkflowEditorPage() {
  const { name } = useParams();
  if (name === undefined) return <EditorScreen initial={emptyEditorState} />;
  return <EditWorkflow name={name} />;
}
