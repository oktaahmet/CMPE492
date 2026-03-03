import { useCallback, useEffect, useMemo, useState } from "react";
import { Wallet, RefreshCw, ListChecks } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { fetchPayments, type PaymentEvent } from "@/lib/api";

type PaymentsHistoryPageProps = {
  workerId: string;
  walletStatus: string;
  onWorkerIdChange: (value: string) => void;
  onConnectWallet: () => Promise<void> | void;
};

function isValidWallet(value: string): boolean {
  const trimmed = value.trim();
  return trimmed.startsWith("0x") && trimmed.length >= 42;
}

function badgeVariantByStatus(status: string) {
  const normalized = status.trim().toLowerCase();
  if (normalized === "confirmed") return "default" as const;
  if (normalized === "retry") return "destructive" as const;
  return "secondary" as const;
}

function badgeClassByStatus(status: string): string {
  const normalized = status.trim().toLowerCase();
  if (normalized === "confirmed") {
    return "bg-emerald-600 hover:bg-emerald-600 text-white";
  }
  return "";
}

function formatDate(input: string): string {
  const date = new Date(input);
  if (Number.isNaN(date.getTime())) {
    return input;
  }
  return date.toLocaleString();
}

export function PaymentsHistoryPage(props: PaymentsHistoryPageProps) {
  const { workerId, walletStatus, onWorkerIdChange, onConnectWallet } = props;
  const [payments, setPayments] = useState<PaymentEvent[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const loadPayments = useCallback(async () => {
    const wallet = workerId.trim();
    if (!isValidWallet(wallet)) {
      setPayments([]);
      setError("Connect wallet first.");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const rows = await fetchPayments(wallet);
      setPayments(rows);
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  }, [workerId]);

  useEffect(() => {
    if (!isValidWallet(workerId)) {
      return;
    }
    void loadPayments();
  }, [workerId, loadPayments]);

  const summary = useMemo(() => {
    let confirmedCount = 0;
    let confirmedUSDC = 0;
    for (const row of payments) {
      if (row.status === "confirmed") {
        confirmedCount += 1;
        const parsed = Number.parseFloat(row.amount_usdc);
        if (Number.isFinite(parsed)) {
          confirmedUSDC += parsed;
        }
      }
    }
    return {
      totalCount: payments.length,
      confirmedCount,
      confirmedUSDC,
    };
  }, [payments]);

  return (
    <>
      <Card className="border-border/70 bg-card/90 backdrop-blur">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <Wallet className="size-4" />
            Payment Wallet
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <Input
            id="paymentWorkerId"
            value={workerId}
            onChange={(event) => onWorkerIdChange(event.target.value)}
            className="font-mono text-xs sm:text-sm"
            placeholder="0x..."
          />
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline">{walletStatus}</Badge>
            <Button type="button" variant="default" onClick={() => void onConnectWallet()}>
              Connect Wallet
            </Button>
            <Button type="button" variant="secondary" onClick={() => void loadPayments()} disabled={loading}>
              <RefreshCw className="size-4" />
              Refresh
            </Button>
          </div>
          {error ? (
            <div className="rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-700">
              {error}
            </div>
          ) : null}
        </CardContent>
      </Card>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <Card className="border-border/70 bg-card/90 backdrop-blur">
          <CardHeader><CardTitle className="text-sm">Total Records</CardTitle></CardHeader>
          <CardContent className="text-2xl font-semibold">{summary.totalCount}</CardContent>
        </Card>
        <Card className="border-border/70 bg-card/90 backdrop-blur">
          <CardHeader><CardTitle className="text-sm">Confirmed Count</CardTitle></CardHeader>
          <CardContent className="text-2xl font-semibold">{summary.confirmedCount}</CardContent>
        </Card>
        <Card className="border-border/70 bg-card/90 backdrop-blur">
          <CardHeader><CardTitle className="text-sm">Confirmed USDC</CardTitle></CardHeader>
          <CardContent className="text-2xl font-semibold">{summary.confirmedUSDC.toFixed(2)}</CardContent>
        </Card>
      </div>

      <Card className="border-border/70 bg-card/90 backdrop-blur">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <ListChecks className="size-4" />
            Payment History
          </CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="text-sm text-muted-foreground">Loading payment history...</div>
          ) : payments.length === 0 ? (
            <div className="text-sm text-muted-foreground">No payment record yet.</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[920px] text-sm">
                <thead>
                  <tr className="border-b text-left text-muted-foreground">
                    <th className="px-2 py-2 font-medium">Updated</th>
                    <th className="px-2 py-2 font-medium">Status</th>
                    <th className="px-2 py-2 font-medium">Amount</th>
                    <th className="px-2 py-2 font-medium">Workflow</th>
                    <th className="px-2 py-2 font-medium">Job</th>
                    <th className="px-2 py-2 font-medium">Tx</th>
                  </tr>
                </thead>
                <tbody>
                  {payments.map((item) => (
                    <tr key={item.id} className="border-b">
                      <td className="px-2 py-2 whitespace-nowrap">{formatDate(item.updated_at)}</td>
                      <td className="px-2 py-2">
                        <Badge variant={badgeVariantByStatus(item.status)} className={badgeClassByStatus(item.status)}>
                          {item.status}
                        </Badge>
                      </td>
                      <td className="px-2 py-2 whitespace-nowrap">{item.amount_usdc} USDC</td>
                      <td className="px-2 py-2">{item.workflow_id}</td>
                      <td className="px-2 py-2">{item.job_id}</td>
                      <td className="px-2 py-2 font-mono text-xs">{item.tx_hash && item.tx_hash !== "" ? item.tx_hash : "-"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </>
  );
}
