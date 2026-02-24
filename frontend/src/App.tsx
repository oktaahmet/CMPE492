import { useCallback, useEffect, useRef, useState } from "react";
import { Activity, Play, Square, Wallet, Waves, Database, ClipboardList } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { fetchPayments, fetchStats, registerWorker } from "./lib/api";
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

export default function App() {
  const [assignmentText, setAssignmentText] = useState("");
  const [logText, setLogText] = useState("");
  const [workerId, setWorkerId] = useState("");
  const [status, setStatus] = useState("idle");
  const [walletStatus, setWalletStatus] = useState("wallet: disconnected");

  const wasmWorkerRef = useRef<Worker | null>(null);
  const runningRef = useRef(false);
  const loopTimerRef = useRef<number | undefined>(undefined);
  const heartbeatTimerRef = useRef<number | undefined>(undefined);
  const discoveredProviderRef = useRef<EIP1193Provider | null>(null);

  const log = useCallback((message: string, obj?: unknown) => {
    const line = obj === undefined ? message : `${message} ${safeStringify(obj)}`;
    setLogText((prev) => `${new Date().toISOString()}  ${line}\n${prev}`);
  }, []);

  const walletAddress = () => workerId.trim();
  const isWalletAddressValid = walletAddress().startsWith("0x") && walletAddress().length >= 42;

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

  const ensureWasmWorker = () => {
    if (wasmWorkerRef.current) {
      return wasmWorkerRef.current;
    }
    wasmWorkerRef.current = new Worker(new URL("./worker-runner.js", import.meta.url));
    log("WASM worker initialized");
    return wasmWorkerRef.current;
  };

  const workLoop = async () => {
    if (!runningRef.current) {
      return;
    }

    try {
      setStatus("working");
      await runWorkerOnce({
        workerID: walletAddress(),
        ensureWasmWorker,
        log,
        setAssignmentText,
      });
    } catch (error) {
      log("Worker loop error", { error: String(error) });
      setStatus("error");
    } finally {
      if (runningRef.current) {
        loopTimerRef.current = window.setTimeout(() => {
          void workLoop();
        }, 1800);
      }
    }
  };

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
        coinbaseWalletExtension: Boolean(window.coinbaseWalletExtension),
        eip6963_discovered: Boolean(discoveredProviderRef.current),
      });
      return;
    }

    try {
      const accounts = await provider.request({ method: "eth_requestAccounts" });
      if (Array.isArray(accounts) && accounts.length > 0 && typeof accounts[0] === "string") {
        setWorkerId(accounts[0]);
        setWalletStatus(`wallet: ${accounts[0]}`);
        log("Wallet connected", { address: accounts[0] });
      }
    } catch (error) {
      log("Wallet connect failed", { error: String(error) });
    }
  };

  const startWorking = () => {
    if (runningRef.current) {
      return;
    }

    runningRef.current = true;
    setStatus("starting");

    heartbeatTimerRef.current = window.setInterval(() => {
      void registerWorker(walletAddress()).catch((error) => {
        log("Heartbeat failed", { error: String(error) });
      });
    }, 15000);

    void workLoop();
  };

  const stopWorking = () => {
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
  };

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
    });

    return () => {
      window.removeEventListener("eip6963:announceProvider", onAnnounceProvider);
      stopWorking();
      wasmWorkerRef.current?.terminate();
      wasmWorkerRef.current = null;
    };
  }, [log]);

  return (
    <main className="relative min-h-screen overflow-hidden px-4 py-8 sm:px-8">
      <div className="pointer-events-none absolute -left-24 -top-20 h-64 w-64 rounded-full bg-cyan-400/25 blur-3xl" />
      <div className="pointer-events-none absolute -right-20 top-16 h-72 w-72 rounded-full bg-amber-300/20 blur-3xl" />
      <div className="pointer-events-none absolute bottom-8 left-1/2 h-56 w-56 -translate-x-1/2 rounded-full bg-emerald-400/20 blur-3xl" />

      <div className="relative mx-auto flex w-full max-w-6xl flex-col gap-4">
        <Card className="border-border/70 bg-card/90 backdrop-blur">
          <CardHeader className="gap-3">
            <CardTitle className="flex items-center gap-2 text-2xl">
              <Waves className="size-5" />
              X402 Browser Worker Console
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-wrap items-center gap-2">
            <Badge variant={statusBadgeVariant()} className="uppercase">
              {status}
            </Badge>
            <Badge variant="outline">{walletStatus}</Badge>
          </CardContent>
        </Card>

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
                onChange={(event) => setWorkerId(event.target.value)}
                className="font-mono text-xs sm:text-sm"
                placeholder="0x..."
              />
              <div className="flex flex-wrap gap-2">
                <Button onClick={() => void connectWallet()} type="button" variant="default">
                  Connect Wallet
                </Button>
                <Button
                  onClick={startWorking}
                  type="button"
                  variant="secondary"
                  disabled={!isWalletAddressValid || runningRef.current}
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
                <Button
                  onClick={() => {
                    void fetchPayments()
                      .then((payments) => {
                        log("Payments", payments);
                      })
                      .catch((error) => {
                        log("Payments fetch failed", { error: String(error) });
                      });
                  }}
                  type="button"
                  variant="ghost"
                >
                  <Database className="size-4" />
                  Fetch Payments
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
              <Textarea
                className="min-h-56 font-mono text-xs"
                value={assignmentText || "{}"}
                readOnly
              />
            </CardContent>
          </Card>
        </div>

        <Card className="border-border/70 bg-card/90 backdrop-blur">
          <CardHeader>
            <CardTitle className="text-base">Live Log Stream</CardTitle>
          </CardHeader>
          <CardContent>
            <Textarea className="min-h-80 font-mono text-xs" value={logText} readOnly />
          </CardContent>
        </Card>
      </div>
    </main>
  );
}
