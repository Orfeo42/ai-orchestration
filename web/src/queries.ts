import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  cancelProcess,
  cancelStepRun,
  deleteWorkflow,
  getCatalog,
  getFailures,
  getProcess,
  getStepRunDiff,
  getStepStats,
  getTokenPoints,
  getWorkflowStats,
  injectNote,
  listPlugins,
  listProcesses,
  listStepRunEvents,
  listWorkflows,
  listWorkflowVersions,
  refreshPlugin,
  registerPlugin,
  removePlugin,
  retryProcess,
  saveWorkflow,
  signalProcess,
  startProcess,
  type DateRange,
  type ProcessFilters,
} from "./api";
import type { Grouping, WorkflowDefinition } from "./process-types";
import { isProcessTerminal } from "./lib/status";

export const POLL_INTERVAL_MS = 5000;
const PAGE_SIZE = 50;

export function useProcesses(filters: ProcessFilters) {
  return useInfiniteQuery({
    queryKey: ["processes", filters],
    queryFn: ({ pageParam }) =>
      listProcesses({ ...filters, limit: PAGE_SIZE, cursor: pageParam }),
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor,
    refetchInterval: POLL_INTERVAL_MS,
  });
}

export function useProcessDetail(id: string) {
  return useQuery({
    queryKey: ["process", id],
    queryFn: () => getProcess(id),
    refetchInterval: (query) => {
      const status = query.state.data?.process.status;
      return status !== undefined && isProcessTerminal(status)
        ? false
        : POLL_INTERVAL_MS;
    },
  });
}

export function useStepRunEvents(stepRunId: string, enabled: boolean) {
  return useQuery({
    queryKey: ["step-run-events", stepRunId],
    queryFn: () => listStepRunEvents(stepRunId),
    enabled,
  });
}

export function useStepRunDiff(stepRunId: string) {
  return useQuery({
    queryKey: ["step-run-diff", stepRunId],
    queryFn: () => getStepRunDiff(stepRunId),
  });
}

export function useWorkflows() {
  return useQuery({ queryKey: ["workflows"], queryFn: listWorkflows });
}

export function useWorkflowVersions(name: string) {
  return useQuery({
    queryKey: ["workflow-versions", name],
    queryFn: () => listWorkflowVersions(name),
    enabled: name !== "",
  });
}

export function useCatalog() {
  return useQuery({ queryKey: ["catalog"], queryFn: getCatalog });
}

export function usePlugins() {
  return useQuery({ queryKey: ["plugins"], queryFn: listPlugins });
}

export function useRegisterPlugin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { name: string; baseUrl: string }) =>
      registerPlugin(input.name, input.baseUrl),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["plugins"] }),
        qc.invalidateQueries({ queryKey: ["catalog"] }),
      ]);
    },
  });
}

export function useRefreshPlugin(name: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => refreshPlugin(name),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["plugins"] }),
        qc.invalidateQueries({ queryKey: ["catalog"] }),
      ]);
    },
  });
}

export function useRemovePlugin(name: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => removePlugin(name),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["plugins"] }),
        qc.invalidateQueries({ queryKey: ["catalog"] }),
      ]);
    },
  });
}

export function useSaveWorkflow() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (definition: WorkflowDefinition) => saveWorkflow(definition),
    onSuccess: async (_saved, definition) => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["workflows"] }),
        qc.invalidateQueries({
          queryKey: ["workflow-versions", definition.name],
        }),
      ]);
    },
  });
}

export function useDeleteWorkflow(name: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => deleteWorkflow(name),
    onSuccess: async () => {
      qc.removeQueries({ queryKey: ["workflow-versions", name] });
      await qc.invalidateQueries({ queryKey: ["workflows"] });
    },
  });
}

export function useWorkflowStats(range: DateRange) {
  return useQuery({
    queryKey: ["analytics", "workflows", range],
    queryFn: () => getWorkflowStats(range),
  });
}

export function useStepStats(workflow: string, range: DateRange) {
  return useQuery({
    queryKey: ["analytics", "steps", workflow, range],
    queryFn: () => getStepStats(workflow, range),
    enabled: workflow !== "",
  });
}

export function useTokenPoints(range: DateRange, group: Grouping) {
  return useQuery({
    queryKey: ["analytics", "tokens", range, group],
    queryFn: () => getTokenPoints(range, group),
  });
}

export function useFailures(range: DateRange) {
  return useQuery({
    queryKey: ["analytics", "failures", range],
    queryFn: () => getFailures(range),
  });
}

export function useStartProcess() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { workflow: string; subjectKey: string }) =>
      startProcess(input.workflow, input.subjectKey),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["processes"] }),
  });
}

export function useCancelProcess(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => cancelProcess(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["process", id] }),
  });
}

export function useRetryProcess(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (step: string) => retryProcess(id, step),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["process", id] }),
  });
}

export function useSignalProcess(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (outcome: string) => signalProcess(id, outcome),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["process", id] }),
  });
}

export function useCancelStepRun(processId: string, stepRunId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => cancelStepRun(stepRunId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["process", processId] }),
  });
}

export function useInjectNote(stepRunId: string) {
  return useMutation({
    mutationFn: (note: string) => injectNote(stepRunId, note),
  });
}
