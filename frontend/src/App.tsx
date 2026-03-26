import { useCallback, useEffect, useRef, useState } from "react";
import { Activity, ClipboardList, Network, Play, Square, Wallet, Waves } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
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

type EIP6963ProviderDetail = {
  info?: {
    name?: string;
    rdns?: string;
  };
  provider?: EIP1193Provider;
};

declare global {
  interface Window {
    ethereum?: EIP1193Provider;
    coinbaseWalletExtension?: EIP1193Provider;
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

export default function App() {
  const autoWorkerModeRef = useRef(isAutoWorkerMode());
  const autoWorkerMode = autoWorkerModeRef.current;
  const initialAuthSessionRef = useRef<WorkerAuthSession | null>(getWorkerAuthSession());
  const initialAuthSession = initialAuthSessionRef.current;
  const [route, setRoute] = useState<"worker" | "payments" | "runtime">(() => {
    if (window.location.hash === "#/payments") return "payments";
    if (window.location.hash === "#/runtime") return "runtime";
    return "worker";
  });
  const [assignmentText, setAssignmentText] = useState("");
  const [logText, setLogText] = useState("");
  const [workerId, setWorkerId] = useState(initialAuthSession?.worker_id ?? "");
  const [authSession, setAuthSession] = useState<WorkerAuthSession | null>(initialAuthSession);
  const [status, setStatus] = useState("idle");
  const [walletStatus, setWalletStatus] = useState(
    initialAuthSession ? `wallet: verified ${initialAuthSession.worker_id}` : "wallet: disconnected",
  );

  const wasmWorkerRef = useRef<Worker | null>(null);
  const workerIdRef = useRef(initialAuthSession?.worker_id ?? "");
  const runningRef = useRef(false);
  const loopTimerRef = useRef<number | undefined>(undefined);
  const heartbeatTimerRef = useRef<number | undefined>(undefined);
  const discoveredProviderRef = useRef<EIP1193Provider | null>(null);

  const log = useCallback((message: string, obj?: unknown) => {
    const line = obj === undefined ? message : `${message} ${safeStringify(obj)}`;
    setLogText((prev) => `${new Date().toISOString()}  ${line}\n${prev}`);
  }, []);

  const walletAddress = () => workerIdRef.current.trim();
  const isWalletAddressValid = isWalletAddress(walletAddress());
  const isAutoWorkerReady = autoWorkerMode && isWalletAddressValid;
  const isWalletVerified =
    authSession !== null && authSession.worker_id.toLowerCase() === walletAddress().toLowerCase();

  const statusBadgeVariant = () => {
    switch (status) {
      case "working":
        return "default" as const;
      case "error":
        return "destructive" as const;
      case "stopped":
        return "outline" as const;
      default:
        return "secondary" as const;
    }
  };

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

  const navigate = (next: "worker" | "payments" | "runtime") => {
    if (next === "payments") {
      window.location.hash = "/payments";
      return;
    }
    if (next === "runtime") {
      window.location.hash = "/runtime";
      return;
    }
    window.location.hash = "/";
  };

  const detectProvider = () => {
    if (discoveredProviderRef.current) {
      return discoveredProviderRef.current;
    }
    const ethereum = window.ethereum;
    if (ethereum) {
      if (Array.isArray(ethereum.providers) && ethereum.providers.length > 0) {
        const coinbase = ethereum.providers.find((provider) => provider?.isCoinbaseWallet);
        return coinbase ?? ethereum.providers[0];
      }
      return ethereum;
    }
    if (window.coinbaseWalletExtension) {
      return window.coinbaseWalletExtension;
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

  const ensureWasmWorker = () => {
    if (wasmWorkerRef.current) {
      return wasmWorkerRef.current;
    }
    wasmWorkerRef.current = new Worker(new URL("./worker-runner.js", import.meta.url));
    log("WASM worker initialized");
    return wasmWorkerRef.current;
  };

  const stopWorking = useCallback(() => {
    runningRef.current = false;
    setStatus("stopped");

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

    try {
      setStatus("working");
      await runWorkerOnce({
        workerID: walletAddress(),
        ensureWasmWorker,
        resetWasmWorker,
        log,
        setAssignmentText,
      });
    } catch (error) {
      handleWorkerAuthFailure("Worker loop error", error);
      setStatus("error");
    } finally {
      if (runningRef.current) {
        loopTimerRef.current = window.setTimeout(() => {
          void workLoop();
        }, 1800);
      }
    }
  }, [ensureWasmWorker, handleWorkerAuthFailure, log, resetWasmWorker]);

  const connectWallet = async () => {
    log("Connect button clicked");

    let provider = detectProvider();
    if (!provider) {
      await new Promise((resolve) => window.setTimeout(resolve, 400));
      provider = detectProvider();
    }
    if (!provider) {
      log("No wallet extension found", {
        ethereum: Boolean(window.ethereum),
        coinbase_wallet_extension: Boolean(window.coinbaseWalletExtension),
        eip6963_discovered: Boolean(discoveredProviderRef.current),
      });
      return;
    }

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

  const startWorking = useCallback(() => {
    if (runningRef.current || !isWalletAddressValid || (!isWalletVerified && !isAutoWorkerReady)) {
      return;
    }

    runningRef.current = true;
    setStatus("starting");

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
      if (String(info.rdns ?? "").includes("coinbase")) {
        discoveredProviderRef.current = provider;
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
      coinbase_wallet_extension: Boolean(window.coinbaseWalletExtension),
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
      if (window.location.hash === "#/payments") {
        setRoute("payments");
        return;
      }
      if (window.location.hash === "#/runtime") {
        setRoute("runtime");
        return;
      }
      setRoute("worker");
    };
    window.addEventListener("hashchange", syncRoute);
    syncRoute();
    return () => {
      window.removeEventListener("hashchange", syncRoute);
    };
  }, []);

  return (
    <main className="relative min-h-screen overflow-hidden px-4 py-8 sm:px-8">
      <div className="pointer-events-none absolute -left-24 -top-20 h-64 w-64 rounded-full bg-cyan-400/25 blur-3xl" />
      <div className="pointer-events-none absolute -right-20 top-16 h-72 w-72 rounded-full bg-amber-300/20 blur-3xl" />
      <div className="pointer-events-none absolute bottom-8 left-1/2 h-56 w-56 -translate-x-1/2 rounded-full bg-emerald-400/20 blur-3xl" />

      <div className={`relative mx-auto flex w-full flex-col gap-4 ${route === "runtime" ? "max-w-[92rem]" : "max-w-6xl"}`}>
        <Card className="border-border/70 bg-card/90 backdrop-blur">
          <CardHeader className="gap-3">
            <CardTitle className="flex items-center gap-2 text-2xl">
              <Waves className="size-5" />
              Browser Worker Interface
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-wrap items-center gap-2">
            <Badge variant={statusBadgeVariant()} className="uppercase">
              {status}
            </Badge>
            <Badge variant="outline">{walletStatus}</Badge>
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
              Runtime
            </Button>
          </CardContent>
        </Card>

        {route === "worker" ? (
          <>
            <div className="grid grid-cols-1 gap-4 lg:grid-cols-[1.1fr_0.9fr]">
              <Card className="border-border/70 bg-card/90 backdrop-blur">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2 text-base">
                    <Wallet className="size-4" />
                    Worker Identity
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  <Input
                    id="workerId"
                    value={workerId}
                    readOnly
                    className="font-mono text-xs sm:text-sm"
                    placeholder="Connect wallet to verify address ownership"
                  />
                  <div className="flex flex-wrap gap-2">
                    <Button onClick={() => void connectWallet()} type="button" variant="default" disabled={autoWorkerMode}>
                      Connect Wallet
                    </Button>
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
          <PaymentsHistoryPage
            workerId={workerId}
            walletStatus={walletStatus}
            onConnectWallet={connectWallet}
          />
        ) : (
          <LiveRuntimePage />
        )}
      </div>
    </main>
  );
}

function sameWorkerAddress(left: string, right: string): boolean {
  return left.trim().toLowerCase() === right.trim().toLowerCase();
}
