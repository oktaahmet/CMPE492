import { useEffect, useMemo, useState } from "react";
import { Network, RefreshCw, RadioTower } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  fetchWorkflowNodeOutput,
  fetchRuntime,
  type JsonObject,
  type RuntimeJobSnapshot,
  type RuntimeSnapshot,
  type RuntimeWorkflowNode,
} from "@/lib/api";

type RuntimeNodeState = "blocked" | "ready" | "assigned" | "submitted" | "finalized";

type PositionedNode = {
  node: RuntimeWorkflowNode;
  job?: RuntimeJobSnapshot;
  state: RuntimeNodeState;
  x: number;
  y: number;
};

type RuntimeEdge = {
  id: string;
  path: string;
  active: boolean;
};

function buildNodeState(node: RuntimeWorkflowNode, job?: RuntimeJobSnapshot): RuntimeNodeState {
  if (node.completed || job?.finalized) {
    return "finalized";
  }
  if ((job?.submitted_workers?.length ?? 0) > 0) {
    return "submitted";
  }
  if ((job?.assigned_workers?.length ?? 0) > 0) {
    return "assigned";
  }
  if (node.enqueued) {
    return "ready";
  }
  return "blocked";
}

function nodeStateClass(state: RuntimeNodeState): string {
  switch (state) {
    case "ready":
      return "border-amber-300 bg-amber-50/95";
    case "assigned":
      return "border-sky-400 bg-sky-50/95 shadow-sky-200/70 animate-pulse";
    case "submitted":
      return "border-violet-400 bg-violet-50/95";
    case "finalized":
      return "border-emerald-400 bg-emerald-50/95";
    default:
      return "border-slate-200 bg-white/95";
  }
}

function isServerNode(node: RuntimeWorkflowNode, job?: RuntimeJobSnapshot): boolean {
  return (job?.execution_target ?? node.execution_target) === "server";
}

function nodeTargetClass(node: RuntimeWorkflowNode, job?: RuntimeJobSnapshot): string {
  if (isServerNode(node, job)) {
    return "ring-2 ring-orange-300/70 ring-offset-2 ring-offset-white shadow-[0_18px_44px_rgba(15,23,42,0.08),0_0_0_1px_rgba(251,146,60,0.18)]";
  }
  return "";
}

function targetChipClass(target?: RuntimeWorkflowNode["execution_target"]): string {
  if (target === "server") {
    return "bg-orange-100 text-orange-800";
  }
  return "bg-sky-100 text-sky-800";
}

function modalHeaderClass(state: RuntimeNodeState, serverNode: boolean): string {
  if (state === "finalized") {
    return serverNode
      ? "border-b border-orange-200/80 bg-[radial-gradient(circle_at_top_left,rgba(249,115,22,0.18),transparent_32%),linear-gradient(180deg,#fffbf5,#fff7ed)]"
      : "border-b border-emerald-200/80 bg-[radial-gradient(circle_at_top_left,rgba(16,185,129,0.16),transparent_35%),linear-gradient(180deg,#fbfffd,#ecfdf5)]";
  }
  if (state === "submitted") {
    return "border-b border-violet-200/80 bg-[radial-gradient(circle_at_top_left,rgba(139,92,246,0.16),transparent_35%),linear-gradient(180deg,#fdfcff,#f5f3ff)]";
  }
  if (state === "assigned") {
    return "border-b border-sky-200/80 bg-[radial-gradient(circle_at_top_left,rgba(59,130,246,0.16),transparent_35%),linear-gradient(180deg,#ffffff,#eff6ff)]";
  }
  if (state === "ready") {
    return "border-b border-amber-200/80 bg-[radial-gradient(circle_at_top_left,rgba(245,158,11,0.16),transparent_35%),linear-gradient(180deg,#fffef9,#fffbeb)]";
  }
  return "border-b border-slate-200/80 bg-[radial-gradient(circle_at_top_left,rgba(148,163,184,0.14),transparent_35%),linear-gradient(180deg,#ffffff,#f8fafc)]";
}

function formatPolicy(policy?: RuntimeWorkflowNode["acceptance_policy"]): string {
  if (policy === "collect_all") {
    return "collect_all";
  }
  return "consensus";
}

function formatTarget(target?: RuntimeWorkflowNode["execution_target"]): string {
  if (target === "server") {
    return "server";
  }
  return "browser";
}

function formatJSONPreview(value: unknown): string {
  if (value === undefined || value === null) {
    return "";
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function depthOfNode(nodeID: string, nodeMap: Map<string, RuntimeWorkflowNode>, memo: Map<string, number>): number {
  const cached = memo.get(nodeID);
  if (cached !== undefined) {
    return cached;
  }

  const node = nodeMap.get(nodeID);
  if (!node || !node.depends_on || node.depends_on.length === 0) {
    memo.set(nodeID, 0);
    return 0;
  }

  const depth = Math.max(...node.depends_on.map((depID) => depthOfNode(depID, nodeMap, memo))) + 1;
  memo.set(nodeID, depth);
  return depth;
}

function computeGraph(runtime: RuntimeSnapshot | null): {
  width: number;
  height: number;
  nodes: PositionedNode[];
  edges: RuntimeEdge[];
  dense: boolean;
  edgesHidden: boolean;
} | null {
  if (!runtime?.workflow?.nodes || runtime.workflow.nodes.length === 0) {
    return null;
  }

  const nodes = runtime.workflow.nodes;
  const topoOrder = runtime.workflow.topological_order ?? [];
  const topoIndex = new Map(topoOrder.map((nodeID, index) => [nodeID, index]));
  const jobsByNodeID = new Map((runtime.jobs ?? []).map((job) => [job.node_id, job]));
  const nodeMap = new Map(nodes.map((node) => [node.id, node]));
  const memo = new Map<string, number>();
  const levels = new Map<number, RuntimeWorkflowNode[]>();

  for (const node of nodes) {
    const depth = depthOfNode(node.id, nodeMap, memo);
    const list = levels.get(depth) ?? [];
    list.push(node);
    levels.set(depth, list);
  }

  for (const list of levels.values()) {
    list.sort((left, right) => {
      const leftIndex = topoIndex.get(left.id) ?? 0;
      const rightIndex = topoIndex.get(right.id) ?? 0;
      return leftIndex - rightIndex;
    });
  }

  const dense = nodes.length > 80;
  const columnWidth = dense ? 164 : 248;
  const rowHeight = dense ? 88 : 168;
  const nodeWidth = dense ? 148 : 188;
  const nodeHeight = dense ? 72 : 116;
  const levelLayout = new Map<number, { x: number; rows: number; columns: number }>();
  const orderedDepths = Array.from(levels.keys()).sort((left, right) => left - right);
  let cursorX = 48;
  let rowCount = 1;

  for (const depth of orderedDepths) {
    const list = levels.get(depth) ?? [];
    const rows = dense ? Math.max(1, Math.ceil(Math.sqrt(list.length))) : Math.max(1, list.length);
    const columns = Math.max(1, Math.ceil(list.length / rows));
    levelLayout.set(depth, { x: cursorX, rows, columns });
    rowCount = Math.max(rowCount, rows);
    cursorX += columns * columnWidth + (dense ? 72 : 96);
  }

  const width = Math.max(760, cursorX + 48);
  const height = Math.max(320, rowCount * rowHeight + 96);

  const positionedNodes: PositionedNode[] = [];
  const positions = new Map<string, { x: number; y: number; state: RuntimeNodeState }>();
  for (const [depth, list] of levels.entries()) {
    list.forEach((node, index) => {
      const job = jobsByNodeID.get(node.id);
      const state = buildNodeState(node, job);
      const layout = levelLayout.get(depth) ?? { x: 48, rows: Math.max(1, list.length), columns: 1 };
      const col = dense ? Math.floor(index / layout.rows) : 0;
      const row = dense ? index % layout.rows : index;
      const x = layout.x + col * columnWidth;
      const y = 52 + row * rowHeight;
      positionedNodes.push({ node, job, state, x, y });
      positions.set(node.id, { x, y, state });
    });
  }

  const edges: RuntimeEdge[] = [];
  const edgeCount = nodes.reduce((sum, node) => sum + (node.depends_on?.length ?? 0), 0);
  const edgesHidden = edgeCount > 1200;
  if (!edgesHidden) {
    for (const node of nodes) {
      const target = positions.get(node.id);
      if (!target) continue;
      for (const depID of node.depends_on ?? []) {
        const source = positions.get(depID);
        if (!source) continue;
        const startX = source.x + nodeWidth;
        const startY = source.y + nodeHeight / 2;
        const endX = target.x;
        const endY = target.y + nodeHeight / 2;
        const midX = (startX + endX) / 2;
        const active = source.state === "finalized" || target.state === "assigned" || target.state === "submitted";
        edges.push({
          id: `${depID}->${node.id}`,
          path: `M ${startX} ${startY} C ${midX} ${startY}, ${midX} ${endY}, ${endX} ${endY}`,
          active,
        });
      }
    }
  }

  return {
    width,
    height,
    nodes: positionedNodes,
    edges,
    dense,
    edgesHidden,
  };
}

export function LiveRuntimePage() {
  const [runtime, setRuntime] = useState<RuntimeSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState("");
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [updatedAt, setUpdatedAt] = useState("");
  const [selectedNodeID, setSelectedNodeID] = useState<string | null>(null);
  const [selectedOutput, setSelectedOutput] = useState<JsonObject | null>(null);
  const [selectedOutputLoading, setSelectedOutputLoading] = useState(false);
  const [selectedOutputError, setSelectedOutputError] = useState("");

  const graph = useMemo(() => computeGraph(runtime), [runtime]);
  const selectedNode = useMemo(
    () => graph?.nodes.find(({ node }) => node.id === selectedNodeID) ?? null,
    [graph, selectedNodeID],
  );
  const selectedTarget = selectedNode?.job?.execution_target ?? selectedNode?.node.execution_target;
  const selectedServerNode = selectedNode ? isServerNode(selectedNode.node, selectedNode.job) : false;

  useEffect(() => {
    let intervalID = 0;
    if (autoRefresh) {
      intervalID = window.setInterval(() => {
        void loadRuntime(true);
      }, 1500);
    }
    return () => {
      if (intervalID) {
        window.clearInterval(intervalID);
      }
    };
  }, [autoRefresh]);

  useEffect(() => {
    void loadRuntime(false);
  }, []);

  useEffect(() => {
    const workflowID = runtime?.workflow?.workflow_id || runtime?.active_workflow_id || runtime?.loaded_workflow_id || "";
    if (!selectedNodeID || !selectedNode?.state || selectedNode.state !== "finalized" || !workflowID) {
      setSelectedOutput(null);
      setSelectedOutputError("");
      setSelectedOutputLoading(false);
      return;
    }

    let cancelled = false;
    setSelectedOutputLoading(true);
    setSelectedOutputError("");
    fetchWorkflowNodeOutput(workflowID, selectedNodeID)
      .then((output) => {
        if (!cancelled) {
          setSelectedOutput(output);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setSelectedOutput(null);
          setSelectedOutputError(String(err));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setSelectedOutputLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [runtime?.active_workflow_id, runtime?.loaded_workflow_id, runtime?.workflow?.workflow_id, selectedNode?.state, selectedNodeID]);

  async function loadRuntime(silent: boolean) {
    if (silent) {
      setRefreshing(true);
    } else {
      setLoading(true);
    }

    try {
      const snapshot = await fetchRuntime();
      setRuntime(snapshot);
      setUpdatedAt(new Date().toLocaleTimeString());
      setError("");
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }

  const stats = runtime?.stats ?? {
    queued_jobs: 0,
    finalized_jobs: 0,
    workers_online: 0,
    pending_payments: 0,
    total_jobs: 0,
  };

  return (
    <>
      <Card className="overflow-hidden border-border/70 bg-card/90 backdrop-blur">
        <CardHeader className="gap-3">
          <CardTitle className="flex flex-wrap items-center gap-3 text-base sm:text-lg">
            <span className="flex items-center gap-2">
              <Network className="size-4" />
              Live Workflow
            </span>
            <Badge variant="outline" className="font-mono text-[11px]">
              workflow={runtime?.active_workflow_id || runtime?.loaded_workflow_id || "-"}
            </Badge>
          </CardTitle>
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <Badge variant="outline" className="text-[11px]">
              topology={runtime?.topology_mode || "-"}
            </Badge>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
            <div className="rounded-2xl border border-slate-200/80 bg-linear-to-b from-white to-slate-50 p-4">
              <div className="text-2xl font-semibold">{stats.queued_jobs}</div>
              <div className="text-xs text-muted-foreground">Queued Jobs</div>
            </div>
            <div className="rounded-2xl border border-slate-200/80 bg-linear-to-b from-white to-emerald-50 p-4">
              <div className="text-2xl font-semibold">{stats.finalized_jobs}</div>
              <div className="text-xs text-muted-foreground">Finalized Jobs</div>
            </div>
            <div className="rounded-2xl border border-slate-200/80 bg-linear-to-b from-white to-sky-50 p-4">
              <div className="text-2xl font-semibold">{stats.workers_online}</div>
              <div className="text-xs text-muted-foreground">Workers Online</div>
            </div>
            <div className="rounded-2xl border border-slate-200/80 bg-linear-to-b from-white to-amber-50 p-4">
              <div className="text-2xl font-semibold">{stats.pending_payments}</div>
              <div className="text-xs text-muted-foreground">Pending Payments</div>
            </div>
            <div className="rounded-2xl border border-slate-200/80 bg-linear-to-b from-white to-violet-50 p-4">
              <div className="text-2xl font-semibold">{stats.total_jobs}</div>
              <div className="text-xs text-muted-foreground">Known Jobs</div>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2 text-sm">
            <Button type="button" variant="secondary" onClick={() => void loadRuntime(true)} disabled={refreshing}>
              <RefreshCw className={`size-4 ${refreshing ? "animate-spin" : ""}`} />
              Refresh
            </Button>
            <Button
              type="button"
              variant={autoRefresh ? "default" : "outline"}
              onClick={() => setAutoRefresh((prev) => !prev)}
            >
              <RadioTower className="size-4" />
              {autoRefresh ? "Auto Refresh On" : "Auto Refresh Off"}
            </Button>
            <span className="text-muted-foreground">
              {loading ? "Loading runtime snapshot..." : `Updated ${updatedAt || "-"}`}
            </span>
          </div>

          {error ? (
            <div className="rounded-2xl border border-red-300 bg-red-50 px-4 py-3 text-sm text-red-700">
              {error}
            </div>
          ) : null}
        </CardContent>
      </Card>

      <div className="grid grid-cols-1 gap-4">
        <Card className="overflow-hidden border-border/70 bg-card/90 backdrop-blur">
          <CardHeader>
            <CardTitle className="flex flex-wrap items-center gap-2 text-base">
              Workflow Graph
              {graph?.dense ? (
                <Badge variant="outline" className="text-[11px]">
                  compact
                </Badge>
              ) : null}
              {graph?.edgesHidden ? (
                <Badge variant="outline" className="text-[11px]">
                  edges hidden
                </Badge>
              ) : null}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {!graph ? (
              <div className="rounded-3xl border border-dashed border-slate-300 bg-slate-50/80 px-6 py-12 text-sm text-muted-foreground">
                No active workflow snapshot available.
              </div>
            ) : (
              <div className="h-[72vh] max-h-[760px] min-h-[360px] overflow-auto overscroll-contain rounded-2xl border border-slate-200/80 bg-[linear-gradient(180deg,#fcfdff,#f4f8ff)] p-2 md:p-3">
                <div className="relative" style={{ width: `${graph.width}px`, height: `${graph.height}px` }}>
                  <svg className="absolute inset-0 h-full w-full" viewBox={`0 0 ${graph.width} ${graph.height}`}>
                    {graph.edges.map((edge) => (
                      <path
                        key={edge.id}
                        d={edge.path}
                        fill="none"
                        stroke={edge.active ? "#0f766e" : "#94a3b8"}
                        strokeWidth={edge.active ? 3 : 2}
                        strokeDasharray={edge.active ? "8 8" : "0"}
                        className={edge.active ? "runtime-edge-flow" : ""}
                        strokeLinecap="round"
                      />
                    ))}
                  </svg>

                  {graph.nodes.map(({ node, job, state, x, y }) => {
                    const hasWorkerChips =
                      (job?.assigned_workers?.length ?? 0) > 0 || (job?.submitted_workers?.length ?? 0) > 0;
                    const target = job?.execution_target ?? node.execution_target;
                    const serverNode = isServerNode(node, job);
                    const traitCount = (job?.traits ?? node.traits ?? []).length;

                    return (
                      <button
                        type="button"
                        key={node.id}
                        className={`absolute overflow-hidden border text-left shadow-[0_12px_32px_rgba(15,23,42,0.07)] transition-transform duration-200 hover:-translate-y-0.5 focus:outline-none focus:ring-2 focus:ring-slate-400/60 ${
                          graph.dense ? "h-[72px] w-[148px] rounded-xl px-3 py-2" : "w-[188px] rounded-[1.25rem] px-4 py-3"
                        } ${nodeStateClass(state)} ${nodeTargetClass(node, job)}`}
                        style={{ left: `${x}px`, top: `${y}px` }}
                        onClick={() => setSelectedNodeID(node.id)}
                      >
                        {serverNode ? (
                          <div className="absolute inset-x-0 top-0 h-1.5 bg-linear-to-r from-orange-500 via-amber-400 to-orange-300" />
                        ) : (
                          <div className="absolute inset-x-0 top-0 h-1 bg-linear-to-r from-sky-300 via-cyan-200 to-sky-100 opacity-70" />
                        )}
                        <div className="flex items-start justify-between gap-2">
                          <div>
                            <div className={`${graph.dense ? "max-w-[92px] truncate text-xs" : "text-sm"} font-semibold`}>
                              {node.id}
                            </div>
                            <div className={`${graph.dense ? "text-[9px] tracking-[0.14em]" : "text-[11px] tracking-[0.18em]"} uppercase text-muted-foreground`}>
                              {state}
                            </div>
                          </div>
                          <Badge variant="outline" className={graph.dense ? "px-1.5 py-0 text-[9px]" : "text-[10px]"}>
                            P{node.priority ?? 0}
                          </Badge>
                        </div>

                        <div className={`${graph.dense ? "mt-2 gap-1 text-[10px]" : "mt-3 gap-2 text-[11px]"} flex flex-wrap text-muted-foreground`}>
                          {!serverNode ? (
                            graph.dense ? null : (
                              <span className="rounded-full bg-white/80 px-2 py-1">reward {node.reward_usdc}</span>
                            )
                          ) : null}
                          <span className={`rounded-full px-2 py-1 font-medium ${targetChipClass(target)}`}>
                            {formatTarget(target)}
                          </span>
                          {!serverNode ? (
                            <>
                              {graph.dense ? (
                                <span className="rounded-full bg-white/80 px-2 py-1">
                                  {job?.accepted_workers ?? 0}/{job?.required_replicas ?? node.replication_factor ?? 1}
                                </span>
                              ) : (
                                <>
                                  <span className="rounded-full bg-white/80 px-2 py-1">
                                    rep {job?.required_replicas ?? node.replication_factor ?? 1}
                                  </span>
                                  <span className="rounded-full bg-white/80 px-2 py-1">
                                    {formatPolicy(job?.acceptance_policy ?? node.acceptance_policy)}
                                  </span>
                                </>
                              )}
                            </>
                          ) : null}
                          {!serverNode && !graph.dense ? (
                            <>
                              <span className="rounded-full bg-white/80 px-2 py-1">
                                assigned {job?.assigned_workers?.length ?? 0}
                              </span>
                              <span className="rounded-full bg-white/80 px-2 py-1">
                                submitted {job?.submitted_workers?.length ?? 0}
                              </span>
                              <span className="rounded-full bg-white/80 px-2 py-1">
                                accepted {job?.accepted_workers ?? 0}
                              </span>
                            </>
                          ) : null}
                        </div>

                        {!graph.dense ? (
                          <div className="mt-3 flex items-center justify-between text-[11px] text-slate-600">
                            <span>{traitCount > 0 ? `${traitCount} tags` : ""}</span>
                            <span>
                              {serverNode ? "tap for output" : hasWorkerChips ? "tap for workers" : "tap for details"}
                            </span>
                          </div>
                        ) : null}
                      </button>
                    );
                  })}
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {selectedNode ? (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/25 p-4 backdrop-blur-sm"
          onClick={() => setSelectedNodeID(null)}
        >
          <div
            className="max-h-[88vh] w-full max-w-4xl overflow-hidden rounded-[2rem] border border-slate-200 bg-white/95 shadow-[0_30px_120px_rgba(15,23,42,0.25)]"
            onClick={(event) => event.stopPropagation()}
          >
            <div className={`${modalHeaderClass(selectedNode.state, selectedServerNode)} px-6 py-5`}>
              <div className="flex items-start justify-between gap-4">
                <div>
                  <div className="text-xl font-semibold text-slate-950">{selectedNode.node.id}</div>
                  <div className="mt-1 text-xs uppercase tracking-[0.22em] text-slate-500">{selectedNode.state}</div>
                </div>
                <Button type="button" variant="outline" onClick={() => setSelectedNodeID(null)}>
                  Close
                </Button>
              </div>

              <div className="mt-4 flex flex-wrap gap-2 text-xs">
                <span className={`rounded-full px-3 py-1 font-medium ${targetChipClass(selectedTarget)}`}>
                  {formatTarget(selectedTarget)}
                </span>
                {!selectedServerNode ? (
                  <>
                    <span className="rounded-full bg-slate-100 px-3 py-1">
                      reward {selectedNode.node.reward_usdc}
                    </span>
                    <span className="rounded-full bg-slate-100 px-3 py-1">
                      rep {selectedNode.job?.required_replicas ?? selectedNode.node.replication_factor ?? 1}
                    </span>
                    <span className="rounded-full bg-slate-100 px-3 py-1">
                      {formatPolicy(selectedNode.job?.acceptance_policy ?? selectedNode.node.acceptance_policy)}
                    </span>
                  </>
                ) : null}
                <span className="rounded-full bg-slate-100 px-3 py-1">priority {selectedNode.node.priority ?? 0}</span>
              </div>
            </div>

            <div className="max-h-[calc(88vh-150px)] space-y-5 overflow-auto px-6 py-5">
              {!selectedServerNode ? (
                <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
                  <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3">
                    <div className="text-xl font-semibold">{selectedNode.job?.assigned_workers?.length ?? 0}</div>
                    <div className="text-xs text-slate-500">Assigned</div>
                  </div>
                  <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3">
                    <div className="text-xl font-semibold">{selectedNode.job?.submitted_workers?.length ?? 0}</div>
                    <div className="text-xs text-slate-500">Submitted</div>
                  </div>
                  <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3">
                    <div className="text-xl font-semibold">{selectedNode.job?.accepted_workers ?? 0}</div>
                    <div className="text-xs text-slate-500">Accepted</div>
                  </div>
                  <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3">
                    <div className="text-xl font-semibold">
                      {(selectedNode.job?.traits ?? selectedNode.node.traits ?? []).length}
                    </div>
                    <div className="text-xs text-slate-500">Traits</div>
                  </div>
                </div>
              ) : null}

              {(selectedNode.job?.traits ?? selectedNode.node.traits ?? []).length > 0 ? (
                <div>
                  <div className="mb-2 text-sm font-medium text-slate-700">Traits</div>
                  <div className="flex flex-wrap gap-2">
                    {(selectedNode.job?.traits ?? selectedNode.node.traits ?? []).map((trait) => (
                      <span key={`${selectedNode.node.id}-modal-trait-${trait}`} className="rounded-full bg-slate-100 px-3 py-1 text-xs text-slate-700">
                        {trait}
                      </span>
                    ))}
                  </div>
                </div>
              ) : null}

              {!selectedServerNode ? (
                <div className="grid grid-cols-1 gap-5 md:grid-cols-2">
                  <div>
                    <div className="mb-2 text-sm font-medium text-slate-700">Assigned Workers</div>
                    <div className="max-h-56 overflow-y-auto overflow-x-hidden rounded-2xl border border-slate-200 bg-slate-50 p-3">
                      {(selectedNode.job?.assigned_workers?.length ?? 0) > 0 ? (
                        <div className="space-y-2">
                          {selectedNode.job?.assigned_workers?.map((workerID) => (
                            <div
                              key={`${selectedNode.node.id}-modal-assigned-${workerID}`}
                              className="w-full break-all rounded-2xl bg-sky-100 px-3 py-2 font-mono text-xs text-sky-900"
                            >
                              {workerID}
                            </div>
                          ))}
                        </div>
                      ) : (
                        <div className="text-sm text-slate-500">No assigned workers.</div>
                      )}
                    </div>
                  </div>

                  <div>
                    <div className="mb-2 text-sm font-medium text-slate-700">Submitted Workers</div>
                    <div className="max-h-56 overflow-y-auto overflow-x-hidden rounded-2xl border border-slate-200 bg-slate-50 p-3">
                      {(selectedNode.job?.submitted_workers?.length ?? 0) > 0 ? (
                        <div className="space-y-2">
                          {selectedNode.job?.submitted_workers?.map((workerID) => (
                            <div
                              key={`${selectedNode.node.id}-modal-submitted-${workerID}`}
                              className="w-full break-all rounded-2xl bg-violet-100 px-3 py-2 font-mono text-xs text-violet-900"
                            >
                              {workerID}
                            </div>
                          ))}
                        </div>
                      ) : (
                        <div className="text-sm text-slate-500">No submitted workers.</div>
                      )}
                    </div>
                  </div>
                </div>
              ) : null}

              <div>
                <div className="mb-2 text-sm font-medium text-slate-700">Final Output</div>
                <div className="rounded-2xl border border-slate-200 bg-slate-950 p-4 text-slate-100">
                  {selectedNode.state !== "finalized" ? (
                    <div className="text-sm text-slate-400">Node is not finalized yet.</div>
                  ) : selectedOutputLoading ? (
                    <div className="text-sm text-slate-400">Loading output...</div>
                  ) : selectedOutputError ? (
                    <div className="text-sm text-red-200">{selectedOutputError}</div>
                  ) : selectedOutput ? (
                    <pre className="max-h-80 overflow-auto whitespace-pre-wrap-break-word font-mono text-xs leading-relaxed">
                      {formatJSONPreview(selectedOutput)}
                    </pre>
                  ) : (
                    <div className="text-sm text-slate-400">No output available.</div>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>
      ) : null}
    </>
  );
}
