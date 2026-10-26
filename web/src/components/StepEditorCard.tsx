import { Trash2 } from "lucide-react";
import { FilterSelect } from "@/components/FilterSelect";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import type { EditorStep, StepErrors } from "@/lib/workflowEditor";
import type { CatalogStep } from "@/types";

type Props = {
  step: EditorStep;
  steps: readonly EditorStep[];
  catalogSteps: readonly CatalogStep[];
  errors: StepErrors;
  onUpdate: (key: string, patch: Partial<EditorStep>) => void;
  onUses: (key: string, uses: string) => void;
  onTransition: (key: string, outcome: string, target: string) => void;
  onRemove: (key: string) => void;
};

export function StepEditorCard({
  step,
  steps,
  catalogSteps,
  errors,
  onUpdate,
  onUses,
  onTransition,
  onRemove,
}: Props) {
  const catalogStep = catalogSteps.find((c) => c.name === step.uses);
  const outcomes = catalogStep?.outcomes ?? Object.keys(step.on);
  const usesOptions = catalogSteps.map((c) => ({
    value: c.name,
    label: c.name,
  }));
  if (step.uses !== "" && catalogStep === undefined) {
    usesOptions.push({ value: step.uses, label: step.uses });
  }
  const targetOptions = [
    ...steps.map((s) => ({
      value: s.key,
      label: s.id.trim() === "" ? "(unnamed)" : s.id,
    })),
    { value: "end", label: "end" },
    { value: "fail", label: "fail" },
  ];

  return (
    <Card role="group" aria-label={`Step ${step.id}`}>
      <CardHeader className="flex-row items-center justify-between pb-3">
        <span className="font-medium">{step.id || "(unnamed)"}</span>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => {
            onRemove(step.key);
          }}
        >
          <Trash2 />
          Remove step
        </Button>
      </CardHeader>
      <CardContent className="grid gap-3">
        <div className="grid gap-2">
          <Label htmlFor={`${step.key}-id`}>Step id</Label>
          <Input
            id={`${step.key}-id`}
            value={step.id}
            aria-invalid={errors.id !== undefined}
            onChange={(e) => {
              onUpdate(step.key, { id: e.target.value });
            }}
          />
          {errors.id !== undefined && (
            <p className="text-xs text-destructive">{errors.id}</p>
          )}
        </div>
        <div className="grid gap-2">
          <Label htmlFor={`${step.key}-description`}>Description</Label>
          <Input
            id={`${step.key}-description`}
            value={step.description}
            onChange={(e) => {
              onUpdate(step.key, { description: e.target.value });
            }}
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor={`${step.key}-uses`}>Uses</Label>
          <FilterSelect
            id={`${step.key}-uses`}
            label="Uses"
            value={step.uses}
            options={usesOptions}
            onChange={(uses) => {
              onUses(step.key, uses);
            }}
          />
          {catalogStep !== undefined && (
            <p className="text-xs text-muted-foreground">
              {catalogStep.description}
            </p>
          )}
        </div>
        <div className="grid gap-2">
          <Label htmlFor={`${step.key}-with`}>With (JSON)</Label>
          <Textarea
            id={`${step.key}-with`}
            className="font-mono text-xs"
            value={step.withText}
            aria-invalid={errors.with !== undefined}
            onChange={(e) => {
              onUpdate(step.key, { withText: e.target.value });
            }}
          />
          {errors.with !== undefined && (
            <p className="text-xs text-destructive">{errors.with}</p>
          )}
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div className="grid gap-2">
            <Label htmlFor={`${step.key}-timeout`}>Timeout</Label>
            <Input
              id={`${step.key}-timeout`}
              value={step.timeout}
              placeholder="30m"
              onChange={(e) => {
                onUpdate(step.key, { timeout: e.target.value });
              }}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor={`${step.key}-retry`}>Retry</Label>
            <Input
              id={`${step.key}-retry`}
              type="number"
              min={0}
              value={step.retry}
              aria-invalid={errors.retry !== undefined}
              onChange={(e) => {
                onUpdate(step.key, { retry: e.target.value });
              }}
            />
            {errors.retry !== undefined && (
              <p className="text-xs text-destructive">{errors.retry}</p>
            )}
          </div>
        </div>
        {outcomes.length > 0 && (
          <div className="grid gap-2">
            <span className="text-sm font-medium">Transitions</span>
            {outcomes.map((outcome) => (
              <div key={outcome} className="flex items-center gap-2">
                <span className="w-32 shrink-0 font-mono text-xs">
                  {outcome}
                </span>
                <FilterSelect
                  label={`On ${outcome}`}
                  value={step.on[outcome] ?? ""}
                  options={targetOptions}
                  onChange={(target) => {
                    onTransition(step.key, outcome, target);
                  }}
                />
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
