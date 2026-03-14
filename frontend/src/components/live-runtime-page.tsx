import { useEffect, useMemo, useState } from "react";
import { Activity, Network, RefreshCw, RadioTower } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  fetchRuntime,
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

function shortenWorkerID(workerID: string): string {
  if (workerID.length <= 12) {
    return workerID;
  }
  return `${workerID.slice(0, 6)}...${workerID.slice(-4)}`;
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

  const columnCount = Math.max(...Array.from(levels.keys())) + 1;
  const rowCount = Math.max(...Array.from(levels.values()).map((list) => list.length));
  const columnWidth = 248;
  const rowHeight = 168;
  const nodeWidth = 188;
  const nodeHeight = 116;
  const width = Math.max(760, columnCount * columnWidth + 96);
  const height = Math.max(320, rowCount * rowHeight + 96);

  const positionedNodes: PositionedNode[] = [];
  const positions = new Map<string, { x: number; y: number; state: RuntimeNodeState }>();
  for (const [depth, list] of levels.entries()) {
    list.forEach((node, index) => {
      const job = jobsByNodeID.get(node.id);
      const state = buildNodeState(node, job);
      const x = 48 + depth * columnWidth;
      const y = 52 + index * rowHeight;
      positionedNodes.push({ node, job, state, x, y });
      positions.set(node.id, { x, y, state });
    });
  }

  const edges: RuntimeEdge[] = [];
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

  return {
    width,
    height,
    nodes: positionedNodes,
    edges,
  };
}

export function LiveRuntimePage() {
  const [runtime, setRuntime] = useState<RuntimeSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState("");
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [updatedAt, setUpdatedAt] = useState("");

  const graph = useMemo(() => computeGraph(runtime), [runtime]);

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

  const recentJobs = [...(runtime?.jobs ?? [])].sort((left, right) => left.queue_index - right.queue_index);

  return (
    <>
      <Card className="overflow-hidden border-border/70 bg-card/90 backdrop-blur">
        <CardHeader className="gap-3">
          <CardTitle className="flex flex-wrap items-center gap-3 text-base sm:text-lg">
            <span className="flex items-center gap-2">
              <Network className="size-4" />
              Live Scheduler Runtime
            </span>
            <Badge variant="outline" className="font-mono text-[11px]">
              active={runtime?.active_workflow_id || "-"}
            </Badge>
            <Badge variant="outline" className="font-mono text-[11px]">
              loaded={runtime?.loaded_workflow_id || "-"}
            </Badge>
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
            <div className="rounded-2xl border border-slate-200/80 bg-gradient-to-b from-white to-slate-50 p-4">
              <div className="text-2xl font-semibold">{stats.queued_jobs}</div>
              <div className="text-xs text-muted-foreground">Queued Jobs</div>
            </div>
            <div className="rounded-2xl border border-slate-200/80 bg-gradient-to-b from-white to-emerald-50 p-4">
              <div className="text-2xl font-semibold">{stats.finalized_jobs}</div>
              <div className="text-xs text-muted-foreground">Finalized Jobs</div>
            </div>
            <div className="rounded-2xl border border-slate-200/80 bg-gradient-to-b from-white to-sky-50 p-4">
              <div className="text-2xl font-semibold">{stats.workers_online}</div>
              <div className="text-xs text-muted-foreground">Workers Online</div>
            </div>
            <div className="rounded-2xl border border-slate-200/80 bg-gradient-to-b from-white to-amber-50 p-4">
              <div className="text-2xl font-semibold">{stats.pending_payments}</div>
              <div className="text-xs text-muted-foreground">Pending Payments</div>
            </div>
            <div className="rounded-2xl border border-slate-200/80 bg-gradient-to-b from-white to-violet-50 p-4">
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

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-[1.35fr_0.65fr]">
        <Card className="overflow-hidden border-border/70 bg-card/90 backdrop-blur">
          <CardHeader>
            <CardTitle className="text-base">Workflow Graph</CardTitle>
          </CardHeader>
          <CardContent>
            {!graph ? (
              <div className="rounded-3xl border border-dashed border-slate-300 bg-slate-50/80 px-6 py-12 text-sm text-muted-foreground">
                No active workflow snapshot available.
              </div>
            ) : (
              <div className="overflow-auto rounded-3xl border border-slate-200/80 bg-[radial-gradient(circle_at_top_left,rgba(59,130,246,0.14),transparent_32%),linear-gradient(180deg,#fcfdff,#f2f7ff)] p-3">
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

                    return (
                      <div
                        key={node.id}
                        className={`absolute w-[188px] rounded-[1.25rem] border px-4 py-3 shadow-[0_18px_44px_rgba(15,23,42,0.08)] transition-transform duration-200 hover:-translate-y-1 ${nodeStateClass(state)}`}
                        style={{ left: `${x}px`, top: `${y}px` }}
                      >
                        <div className="flex items-start justify-between gap-2">
                          <div>
                            <div className="text-sm font-semibold">{node.id}</div>
                            <div className="text-[11px] uppercase tracking-[0.18em] text-muted-foreground">
                              {state}
                            </div>
                          </div>
                          <Badge variant="outline" className="text-[10px]">
                            P{node.priority ?? 0}
                          </Badge>
                        </div>

                        <div className="mt-3 flex flex-wrap gap-2 text-[11px] text-muted-foreground">
                          <span className="rounded-full bg-white/80 px-2 py-1">reward {node.reward_usdc}</span>
                          <span className="rounded-full bg-white/80 px-2 py-1">
                            assigned {job?.assigned_workers?.length ?? 0}
                          </span>
                          <span className="rounded-full bg-white/80 px-2 py-1">
                            submitted {job?.submitted_workers?.length ?? 0}
                          </span>
                        </div>

                        {hasWorkerChips ? (
                          <div className="mt-3 text-[11px] text-slate-700">
                            <div className="mb-1 font-medium text-slate-600">Workers</div>
                            <div className="flex flex-wrap gap-1">
                              {job?.assigned_workers?.map((workerID) => (
                                <span key={`${node.id}-assigned-${workerID}`} className="rounded-full bg-sky-100 px-2 py-1 font-mono">
                                  {shortenWorkerID(workerID)}
                                </span>
                              ))}
                              {job?.submitted_workers?.map((workerID) => (
                                <span key={`${node.id}-submitted-${workerID}`} className="rounded-full bg-violet-100 px-2 py-1 font-mono">
                                  {shortenWorkerID(workerID)}
                                </span>
                              ))}
                            </div>
                          </div>
                        ) : null}
                      </div>
                    );
                  })}
                </div>
              </div>
            )}
          </CardContent>
        </Card>

        <Card className="border-border/70 bg-card/90 backdrop-blur">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Activity className="size-4" />
              Job Activity
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {recentJobs.length === 0 ? (
              <div className="rounded-2xl border border-dashed border-slate-300 bg-slate-50/80 px-4 py-6 text-sm text-muted-foreground">
                No runtime jobs yet.
              </div>
            ) : (
              recentJobs.map((job) => (
                <div key={job.job_id} className="rounded-2xl border border-slate-200 bg-white/80 p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="font-medium">{job.node_id}</div>
                      <div className="font-mono text-[11px] text-muted-foreground">{job.job_id}</div>
                    </div>
                    <Badge variant={job.finalized ? "default" : "outline"}>
                      {job.finalized ? "finalized" : "active"}
                    </Badge>
                  </div>
                  <div className="mt-3 flex flex-wrap gap-2 text-[11px] text-muted-foreground">
                    <span className="rounded-full bg-slate-100 px-2 py-1">
                      queue #{Math.max(job.queue_index, 0)}
                    </span>
                    <span className="rounded-full bg-slate-100 px-2 py-1">
                      accepted {job.accepted_workers}
                    </span>
                    <span className="rounded-full bg-slate-100 px-2 py-1">
                      workers {(job.assigned_workers?.length ?? 0) + (job.submitted_workers?.length ?? 0)}
                    </span>
                  </div>
                </div>
              ))
            )}
          </CardContent>
        </Card>
      </div>
    </>
  );
}
