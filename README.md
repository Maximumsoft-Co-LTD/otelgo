# otelgo
- APP ทุกตัวใช้ otelgo → ส่ง Traces/Metrics/Logs ออกเป็น OTLP
- ทุกอย่างยิงไปที่ OTel Collector ตัวเดียว
- Collector แยกเอา:
- Traces → Jaeger / Tempo
- Metrics → Prometheus → Grafana
- Logs → zap → alloy → Loki
```mermaid
flowchart LR
    %% =========================
    %% Applications
    %% =========================
    subgraph Apps["Applications"]
        A1["APP #1"]
        A2["APP #2"]
        A3["APP #3"]
    end

    %% =========================
    %% SDK / Library ใน App
    %% =========================
    subgraph SDK["OTel SDK + eto"]
        B(("Tracing / Metrics / Logs"))
    end

    %% =========================
    %% OTel Collector
    %% =========================
    subgraph Collector["OTel Collector"]
        COL{{"Collector\n(OTLP Receiver)"}}
    end

    %% =========================
    %% Observability Backends
    %% =========================
    subgraph Tracing["Tracing Backend"]
        T1["Tempo"]
    end

    subgraph Metrics["Metrics Backend"]
        M1["Prometheus"]
    end

    subgraph Logging["Logging Backend"]
        subgraph Alloy["Grafana Alloy"]
            AL{{"OTLP → Loki"}}
        end
        L2["Loki"]
    end

    subgraph GrafanaUI["Grafana"]
        G1["Dashboards / Explore\n(Traces + Metrics + Logs)"]
    end

    %% Stdout debug logs นอก OTLP flow
    subgraph Stdout["App Local Logs"]
        Z1["Zap JSON → stdout\n(docker logs / k8s logs)"]
    end

    %% =========================
    %% Edges
    %% =========================
    A1 --> B
    A2 --> B
    A3 --> B

    %% App ส่ง OTLP (Traces / Metrics / Logs) เข้า Collector
    B -->|"OTLP: Traces / Metrics / Logs"| COL

    %% Traces
    COL -->|"Traces"| T1
    T1 -->|"Tempo datasource"| G1

    %% Metrics
    COL -->|"Metrics"| M1
    M1 -->|"Prometheus datasource"| G1

    %% Logs ผ่าน Alloy → Loki
    COL -->|"Logs (OTLP)"| AL
    AL  -->|"Loki push API"| L2
    L2  -->|"Loki datasource"| G1

    %% Stdout logs (ไม่ผ่าน OTLP)
    B -->|"Zap logger"| Z1
```