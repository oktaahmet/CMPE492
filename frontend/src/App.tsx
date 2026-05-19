import { useCallback, useEffect, useRef, useState } from "react";
import { Activity, ClipboardList, Network, Play, Square, Wallet, Waves } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  clearWorkerAuthSession,
  fetchStats,
  getWorkerAuthSession,
  registerWorker,
  requestWorkerAuthChallenge,
  type WorkerAuthSession,
  verifyWorkerAuthChallenge,
} from "./lib/api";
import { LiveRuntimePage } from "./components/live-runtime-page";
import { PaymentsHistoryPage } from "./components/payments-history-page";
import { runWorkerOnce } from "./lib/worker-loop";

type EIP1193RequestArgs = {
  method: string;
  params?: unknown[] | object;
};

type EIP1193Provider = {
  isCoinbaseWallet?: boolean;
  providers?: EIP1193Provider[];
  request: (args: EIP1193RequestArgs) => Promise<unknown>;
};

type EIP1193ProviderWithEvents = EIP1193Provider & {
  on?: (eventName: string, listener: (...args: unknown[]) => void) => void;
  removeListener?: (eventName: string, listener: (...args: unknown[]) => void) => void;
};

type EIP6963ProviderInfo = {
  uuid?: string;
  name?: string;
  rdns?: string;
  icon?: string;
};

type EIP6963ProviderDetail = {
  info?: EIP6963ProviderInfo;
  provider?: EIP1193Provider;
};

type DiscoveredWallet = {
  info: EIP6963ProviderInfo;
  provider: EIP1193Provider;
};

type AppRoute = "worker" | "payments" | "runtime";

const WORKER_BUSY_LOOP_DELAY_MS = readNonNegativeEnvInt(import.meta.env.VITE_WORKER_BUSY_LOOP_DELAY_MS, 0);
const WORKER_IDLE_DELAY_INITIAL_MS = readNonNegativeEnvInt(import.meta.env.VITE_WORKER_IDLE_DELAY_INITIAL_MS, 250);
const WORKER_IDLE_DELAY_MAX_MS = Math.max(
  WORKER_IDLE_DELAY_INITIAL_MS,
  readNonNegativeEnvInt(import.meta.env.VITE_WORKER_IDLE_DELAY_MAX_MS, 5000),
);
const WORKER_IDLE_DELAY_MULTIPLIER = readMinEnvNumber(import.meta.env.VITE_WORKER_IDLE_DELAY_MULTIPLIER, 2, 1);

function walletKey(info: EIP6963ProviderInfo): string {
  return info.rdns ?? info.uuid ?? info.name ?? "";
}

declare global {
  interface Window {
    ethereum?: EIP1193Provider;
  }
}

function safeStringify(value: unknown): string {
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function isWalletAddress(value: string): boolean {
  return value.startsWith("0x") && value.length >= 42;
}

function compactWalletStatus(status: string): string {
  return status.replace(/\s0x[a-fA-F0-9]{40}/g, "").trim();
}

function readNonNegativeEnvInt(raw: unknown, fallback: number): number {
  const value = typeof raw === "string" ? Number.parseInt(raw, 10) : Number.NaN;
  if (!Number.isFinite(value) || value < 0) {
    return fallback;
  }
  return value;
}

function readMinEnvNumber(raw: unknown, fallback: number, min: number): number {
  const value = typeof raw === "string" ? Number.parseFloat(raw) : Number.NaN;
  if (!Number.isFinite(value) || value < min) {
    return fallback;
  }
  return value;
}

function workerStatusClass(status: string): string {
  switch (status) {
    case "working":
      return "border-emerald-200 bg-emerald-50 text-emerald-700";
    case "error":
      return "border-red-200 bg-red-50 text-red-700";
    default:
      return "border-sky-200 bg-sky-50 text-sky-700";
  }
}

function walletStatusClass(status: string): string {
  const normalized = status.toLowerCase();
  if (normalized.includes("verified")) {
    return "border-emerald-200 bg-emerald-50 text-emerald-700";
  }
  if (normalized.includes("auto worker")) {
    return "border-cyan-200 bg-cyan-50 text-cyan-700";
  }
  if (normalized.includes("failed") || normalized.includes("expired") || normalized.includes("not verified")) {
    return "border-amber-200 bg-amber-50 text-amber-800";
  }
  return "border-slate-200 bg-slate-50 text-slate-600";
}

function isAuthorizationError(error: unknown): boolean {
  const text = String(error);
  return text.includes("401") || text.includes("403");
}

function isAutoWorkerMode(): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  return new URLSearchParams(window.location.search).get("auto_worker") === "1";
}

function generateRandomWorkerAddress(): string {
  const bytes = new Uint8Array(20);
  crypto.getRandomValues(bytes);
  let out = "0x";
  for (const value of bytes) {
    out += value.toString(16).padStart(2, "0");
  }
  return out;
}

async function signWalletMessage(
  provider: EIP1193Provider,
  walletAddress: string,
  message: string,
): Promise<string> {
  try {
    const signature = await provider.request({
      method: "personal_sign",
      params: [message, walletAddress],
    });
    if (typeof signature === "string" && signature.length > 0) {
      return signature;
    }
  } catch {
    const fallbackSignature = await provider.request({
      method: "personal_sign",
      params: [walletAddress, message],
    });
    if (typeof fallbackSignature === "string" && fallbackSignature.length > 0) {
      return fallbackSignature;
    }
  }

  throw new Error("wallet signature missing");
}

function routeFromPath(pathname: string): AppRoute {
  if (pathname === "/payments") return "payments";
  if (pathname === "/runtime") return "runtime";
  return "worker";
}

function pathForRoute(route: AppRoute): string {
  if (route === "payments") return "/payments";
  if (route === "runtime") return "/runtime";
  return "/";
}

function canonicalizeRouteURL(): AppRoute {
  const currentRoute = routeFromPath(window.location.pathname);
  const canonicalPath = pathForRoute(currentRoute);
  const shouldReplace =
    window.location.pathname !== canonicalPath ||
    window.location.hash !== "";

  if (shouldReplace) {
    window.history.replaceState(null, "", `${canonicalPath}${window.location.search}`);
  }

  return currentRoute;
}

export default function App() {
  const autoWorkerModeRef = useRef(isAutoWorkerMode());
  const autoWorkerMode = autoWorkerModeRef.current;
  const initialAuthSessionRef = useRef<WorkerAuthSession | null>(getWorkerAuthSession());
  const initialAuthSession = initialAuthSessionRef.current;
  const [route, setRoute] = useState<AppRoute>(() => canonicalizeRouteURL());
  const [assignmentText, setAssignmentText] = useState("");
  const [logText, setLogText] = useState("");
  const [workerId, setWorkerId] = useState(initialAuthSession?.worker_id ?? "");
  const [authSession, setAuthSession] = useState<WorkerAuthSession | null>(initialAuthSession);
  const [status, setStatus] = useState("idle");
  const [walletStatus, setWalletStatus] = useState(
    initialAuthSession ? `wallet: verified ${initialAuthSession.worker_id}` : "wallet: disconnected",
  );
  const [discoveredWallets, setDiscoveredWallets] = useState<DiscoveredWallet[]>([]);
  const discoveredWalletsRef = useRef<DiscoveredWallet[]>([]);
  const [walletPickerOpen, setWalletPickerOpen] = useState(false);

  const wasmWorkerRef = useRef<Worker | null>(null);
  const workerIdRef = useRef(initialAuthSession?.worker_id ?? "");
  const runningRef = useRef(false);
  const loopTimerRef = useRef<number | undefined>(undefined);
  const heartbeatTimerRef = useRef<number | undefined>(undefined);
  const discoveredProviderRef = useRef<EIP1193Provider | null>(null);
  const idleDelayRef = useRef(WORKER_IDLE_DELAY_INITIAL_MS);

  const log = useCallback((message: string, obj?: unknown) => {
    const line = obj === undefined ? message : `${message} ${safeStringify(obj)}`;
    setLogText((prev) => `${new Date().toISOString()}  ${line}\n${prev}`);
  }, []);

  const walletAddress = () => workerIdRef.current.trim();
  const isWalletAddressValid = isWalletAddress(walletAddress());
  const isAutoWorkerReady = autoWorkerMode && isWalletAddressValid;
  const isWalletVerified =
    authSession !== null && authSession.worker_id.toLowerCase() === walletAddress().toLowerCase();

  const clearAuthState = useCallback((nextStatus?: string) => {
    clearWorkerAuthSession();
    setAuthSession(null);
    setWalletStatus(nextStatus ?? (workerIdRef.current ? `wallet: not verified ${workerIdRef.current}` : "wallet: disconnected"));
  }, []);

  const applyAuthSession = useCallback((session: WorkerAuthSession) => {
    setAuthSession(session);
    workerIdRef.current = session.worker_id;
    setWorkerId(session.worker_id);
    setWalletStatus(`wallet: verified ${session.worker_id}`);
  }, []);

  const navigate = (next: AppRoute) => {
    const path = pathForRoute(next);
    if (window.location.pathname !== path || window.location.hash !== "") {
      window.history.pushState(null, "", `${path}${window.location.search}`);
    }
    setRoute(next);
  };

  const detectProvider = () => {
    if (discoveredProviderRef.current) {
      return discoveredProviderRef.current;
    }
    const ethereum = window.ethereum;
    if (ethereum) {
      if (Array.isArray(ethereum.providers) && ethereum.providers.length > 0) {
        return ethereum.providers[0];
      }
      return ethereum;
    }
    return null;
  };

  const resetWasmWorker = useCallback((reason: string) => {
    if (!wasmWorkerRef.current) {
      return;
    }
    wasmWorkerRef.current.terminate();
    wasmWorkerRef.current = null;
    log("WASM worker reset", { reason });
  }, [log]);

  const ensureWasmWorker = useCallback(() => {
    if (wasmWorkerRef.current) {
      return wasmWorkerRef.current;
    }
    wasmWorkerRef.current = new Worker(new URL("./worker-runner.js", import.meta.url), { type: "module" });
    log("WASM worker initialized");
    return wasmWorkerRef.current;
  }, [log]);

  const stopWorking = useCallback(() => {
    runningRef.current = false;
    setStatus("idle");
    idleDelayRef.current = WORKER_IDLE_DELAY_INITIAL_MS;

    if (loopTimerRef.current !== undefined) {
      window.clearTimeout(loopTimerRef.current);
      loopTimerRef.current = undefined;
    }
    if (heartbeatTimerRef.current !== undefined) {
      window.clearInterval(heartbeatTimerRef.current);
      heartbeatTimerRef.current = undefined;
    }

    resetWasmWorker("stop");
  }, [resetWasmWorker]);

  const handleWorkerAuthFailure = useCallback((message: string, error: unknown) => {
    log(message, { error: String(error) });
    if (isAuthorizationError(error)) {
      stopWorking();
      clearAuthState("wallet: session expired, reconnect required");
    }
  }, [clearAuthState, log, stopWorking]);

  const workLoop = useCallback(async () => {
    if (!runningRef.current) {
      return;
    }

    let nextLoopDelay = WORKER_IDLE_DELAY_INITIAL_MS;
    let advanceIdleDelay = false;
    try {
      setStatus("working");
      const result = await runWorkerOnce({
        workerID: walletAddress(),
        ensureWasmWorker,
        resetWasmWorker,
        log,
        setAssignmentText,
      });
      if (result === "job_completed") {
        idleDelayRef.current = WORKER_IDLE_DELAY_INITIAL_MS;
        nextLoopDelay = WORKER_BUSY_LOOP_DELAY_MS;
        setStatus("working");
      } else {
        nextLoopDelay = idleDelayRef.current;
        advanceIdleDelay = true;
        setStatus("idle");
      }
    } catch (error) {
      handleWorkerAuthFailure("Worker loop error", error);
      nextLoopDelay = idleDelayRef.current;
      advanceIdleDelay = true;
      setStatus("error");
    } finally {
      if (runningRef.current) {
        if (advanceIdleDelay && nextLoopDelay > 0) {
          idleDelayRef.current = Math.min(
            Math.ceil(nextLoopDelay * WORKER_IDLE_DELAY_MULTIPLIER),
            WORKER_IDLE_DELAY_MAX_MS,
          );
        }
        loopTimerRef.current = window.setTimeout(() => {
          void workLoop();
        }, nextLoopDelay);
      }
    }
  }, [ensureWasmWorker, handleWorkerAuthFailure, log, resetWasmWorker]);

  const connectWithProvider = async (provider: EIP1193Provider, walletName: string) => {
    setWalletPickerOpen(false);
    log("Wallet selected", { name: walletName });
    try {
      const accounts = await provider.request({ method: "eth_requestAccounts" });
      if (!Array.isArray(accounts) || accounts.length === 0 || typeof accounts[0] !== "string") {
        throw new Error("wallet account missing");
      }

      const nextWorkerID = accounts[0].trim();
      workerIdRef.current = nextWorkerID;
      setWorkerId(nextWorkerID);
      clearAuthState(`wallet: signature requested ${nextWorkerID}`);

      const challenge = await requestWorkerAuthChallenge(nextWorkerID);
      const signature = await signWalletMessage(provider, nextWorkerID, challenge.message);
      const session = await verifyWorkerAuthChallenge(nextWorkerID, challenge.nonce, signature);
      applyAuthSession(session);
      log("Wallet connected and verified", { address: session.worker_id, expires_at: session.expires_at });
    } catch (error) {
      const currentWorkerID = workerIdRef.current;
      clearAuthState(currentWorkerID ? `wallet: verification failed ${currentWorkerID}` : "wallet: disconnected");
      log("Wallet connect failed", { error: String(error) });
    }
  };

  const connectWallet = async () => {
    log("Connect button clicked");

    // Re-request EIP-6963 announcements; late-loading extensions can still respond.
    window.dispatchEvent(new Event("eip6963:requestProvider"));
    await new Promise((resolve) => window.setTimeout(resolve, 200));

    const wallets = discoveredWalletsRef.current;
    if (wallets.length === 1) {
      await connectWithProvider(wallets[0].provider, wallets[0].info.name ?? "wallet");
      return;
    }
    if (wallets.length >= 2) {
      setWalletPickerOpen(true);
      return;
    }

    const fallback = detectProvider();
    if (!fallback) {
      log("No wallet extension found", {
        ethereum: Boolean(window.ethereum),
        eip6963_discovered: Boolean(discoveredProviderRef.current),
      });
      return;
    }
    await connectWithProvider(fallback, "browser wallet");
  };

  const startWorking = useCallback(() => {
    if (runningRef.current || !isWalletAddressValid || (!isWalletVerified && !isAutoWorkerReady)) {
      return;
    }

    runningRef.current = true;
    setStatus("working");

    heartbeatTimerRef.current = window.setInterval(() => {
      void registerWorker(walletAddress()).catch((error) => {
        handleWorkerAuthFailure("Heartbeat failed", error);
      });
    }, 15000);

    void workLoop();
  }, [handleWorkerAuthFailure, isAutoWorkerReady, isWalletAddressValid, isWalletVerified, workLoop]);

  useEffect(() => {
    workerIdRef.current = workerId;
  }, [workerId]);

  useEffect(() => {
    if (!autoWorkerMode) {
      return;
    }

    const nextWorkerID = generateRandomWorkerAddress();
    clearWorkerAuthSession();
    setAuthSession(null);
    workerIdRef.current = nextWorkerID;
    setWorkerId(nextWorkerID);
    setWalletStatus(`wallet: auto worker ${nextWorkerID}`);
    log("Auto worker mode enabled", { worker_id: nextWorkerID });
  }, [autoWorkerMode, log]);

  useEffect(() => {
    if (!autoWorkerMode || runningRef.current || !isWalletAddressValid) {
      return;
    }

    const timerID = window.setTimeout(() => {
      startWorking();
    }, 150);
    return () => {
      window.clearTimeout(timerID);
    };
  }, [autoWorkerMode, isWalletAddressValid, startWorking, workerId]);

  useEffect(() => {
    const onAnnounceProvider = (event: Event) => {
      const detail = (event as CustomEvent<EIP6963ProviderDetail>).detail ?? {};
      const info = detail.info ?? {};
      const provider = detail.provider;
      if (!provider) {
        return;
      }

      if (!discoveredProviderRef.current) {
        discoveredProviderRef.current = provider;
      }

      const key = walletKey(info);
      const alreadyKnown = discoveredWalletsRef.current.some((w) => walletKey(w.info) === key);
      if (!alreadyKnown) {
        const entry: DiscoveredWallet = { info, provider };
        discoveredWalletsRef.current = [...discoveredWalletsRef.current, entry];
        setDiscoveredWallets(discoveredWalletsRef.current);
      }

      log("EIP-6963 provider announced", {
        name: info.name ?? "unknown",
        rdns: info.rdns ?? "unknown",
      });
    };

    window.addEventListener("eip6963:announceProvider", onAnnounceProvider);
    window.dispatchEvent(new Event("eip6963:requestProvider"));

    log("UI initialized", {
      ethereum_injected: Boolean(window.ethereum),
      worker_auth_restored: Boolean(initialAuthSession),
    });

    const provider = detectProvider() as EIP1193ProviderWithEvents | null;
    const onAccountsChanged = (...args: unknown[]) => {
      const accountList = Array.isArray(args[0]) ? args[0] : [];
      const nextAccount = typeof accountList[0] === "string" ? accountList[0].trim() : "";
      if (!nextAccount) {
        stopWorking();
        workerIdRef.current = "";
        setWorkerId("");
        clearAuthState("wallet: disconnected");
        return;
      }
      if (!sameWorkerAddress(nextAccount, workerIdRef.current)) {
        stopWorking();
        workerIdRef.current = nextAccount;
        setWorkerId(nextAccount);
        clearAuthState(`wallet: account changed ${nextAccount}`);
      }
    };

    provider?.on?.("accountsChanged", onAccountsChanged);

    return () => {
      provider?.removeListener?.("accountsChanged", onAccountsChanged);
      window.removeEventListener("eip6963:announceProvider", onAnnounceProvider);
      stopWorking();
      resetWasmWorker("unmount");
    };
  }, [clearAuthState, log, resetWasmWorker, stopWorking]);

  useEffect(() => {
    const syncRoute = () => {
      setRoute(canonicalizeRouteURL());
    };
    window.addEventListener("popstate", syncRoute);
    syncRoute();
    return () => {
      window.removeEventListener("popstate", syncRoute);
    };
  }, []);

  return (
    <main className="relative min-h-screen overflow-hidden px-4 py-8 sm:px-8">
      <div className="pointer-events-none absolute -left-24 -top-20 h-64 w-64 rounded-full bg-cyan-400/25 blur-3xl" />
      <div className="pointer-events-none absolute -right-20 top-16 h-72 w-72 rounded-full bg-amber-300/20 blur-3xl" />
      <div className="pointer-events-none absolute bottom-8 left-1/2 h-56 w-56 -translate-x-1/2 rounded-full bg-emerald-400/20 blur-3xl" />

      <div className="relative mx-auto flex w-full max-w-368 flex-col gap-4">
        <Card className="border-border/70 bg-card/90 backdrop-blur">
          <CardHeader className="gap-3">
            <CardTitle className="flex flex-wrap items-center gap-3 text-2xl">
              <Waves className="size-5 text-cyan-700" />
              <span>Browser Worker Interface</span>
              <Button
                onClick={() => void connectWallet()}
                type="button"
                variant="default"
                disabled={autoWorkerMode}
                className="ml-0 text-sm sm:ml-3"
              >
                <Wallet className="size-4" />
                Connect Wallet
              </Button>
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-wrap items-center gap-2">
            <Badge variant="outline" className={`uppercase ${workerStatusClass(status)}`}>
              {status}
            </Badge>
            <Badge variant="outline" className={walletStatusClass(walletStatus)}>
              {compactWalletStatus(walletStatus)}
            </Badge>
            {workerId.trim() ? (
              <Badge
                variant="outline"
                className="max-w-full truncate border-slate-200 bg-slate-50 font-mono normal-case text-slate-700"
              >
                {workerId}
              </Badge>
            ) : null}
            <Button
              type="button"
              variant={route === "worker" ? "default" : "outline"}
              onClick={() => navigate("worker")}
            >
              Worker
            </Button>
            <Button
              type="button"
              variant={route === "payments" ? "default" : "outline"}
              onClick={() => navigate("payments")}
            >
              Payments
            </Button>
            <Button
              type="button"
              variant={route === "runtime" ? "default" : "outline"}
              onClick={() => navigate("runtime")}
            >
              <Network className="size-4" />
              Live Workflow
            </Button>
          </CardContent>
        </Card>

        {route === "worker" ? (
          <>
            <div className="grid grid-cols-1 gap-4 lg:grid-cols-[1.1fr_0.9fr]">
              <Card className="border-border/70 bg-card/90 backdrop-blur">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2 text-base">
                    <Activity className="size-4" />
                    Worker Controls
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  <div className="flex flex-wrap gap-2">
                    <Button
                      onClick={startWorking}
                      type="button"
                      variant="secondary"
                      disabled={(!isWalletVerified && !isAutoWorkerReady) || runningRef.current}
                    >
                      <Play className="size-4" />
                      Start
                    </Button>
                    <Button onClick={stopWorking} type="button" variant="outline">
                      <Square className="size-4" />
                      Stop
                    </Button>
                    <Button
                      onClick={() => {
                        void fetchStats()
                          .then((stats) => {
                            log("Stats", stats);
                          })
                          .catch((error) => {
                            log("Stats fetch failed", { error: String(error) });
                          });
                      }}
                      type="button"
                      variant="ghost"
                    >
                      <Activity className="size-4" />
                      Fetch Stats
                    </Button>
                  </div>
                </CardContent>
              </Card>

              <Card className="border-border/70 bg-card/90 backdrop-blur">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2 text-base">
                    <ClipboardList className="size-4" />
                    Current Assignment
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <Textarea className="min-h-56 font-mono text-xs" value={assignmentText || "{}"} readOnly />
                </CardContent>
              </Card>
            </div>

            <Card className="border-border/70 bg-card/90 backdrop-blur">
              <CardHeader>
                <CardTitle className="text-base">Logs</CardTitle>
              </CardHeader>
              <CardContent>
                <Textarea className="min-h-80 font-mono text-xs" value={logText} readOnly />
              </CardContent>
            </Card>
          </>
        ) : route === "payments" ? (
          <PaymentsHistoryPage workerId={workerId} />
        ) : (
          <LiveRuntimePage />
        )}
      </div>

      <AlertDialog open={walletPickerOpen} onOpenChange={setWalletPickerOpen}>
        <AlertDialogContent size="default">
          <AlertDialogHeader>
            <AlertDialogTitle>Select a wallet</AlertDialogTitle>
            <AlertDialogDescription>
              Multiple wallet extensions detected. Choose which one to connect with.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex flex-col gap-2">
            {discoveredWallets.map((wallet, index) => {
              const key = walletKey(wallet.info) || String(index);
              const label = wallet.info.name ?? wallet.info.rdns ?? "Unknown wallet";
              return (
                <Button
                  key={key}
                  type="button"
                  variant="outline"
                  className="h-12 justify-start gap-3"
                  onClick={() => void connectWithProvider(wallet.provider, label)}
                >
                  {wallet.info.icon ? (
                    <img src={wallet.info.icon} alt="" className="size-6 rounded" />
                  ) : (
                    <Wallet className="size-5" />
                  )}
                  <span className="truncate">{label}</span>
                </Button>
              );
            })}
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </main>
  );
}

function sameWorkerAddress(left: string, right: string): boolean {
  return left.trim().toLowerCase() === right.trim().toLowerCase();
}
