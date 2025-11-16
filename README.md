# otelgo
- APP ทุกตัวใช้ otelgo → ส่ง Traces/Metrics/Logs ออกเป็น OTLP
- ทุกอย่างยิงไปที่ OTel Collector ตัวเดียว
- Collector แยกเอา:
- Traces → Jaeger / Tempo
- Metrics → Prometheus → Grafana
- Logs → sLog / Loki (หรือส่งต่อไป Alloy / Agent ก่อนก็ได้)
- ส่วน Zap เป็น logger ใน app → พ่น JSON ลง stdout แล้วให้ Alloy/Promtail ดึงเข้า Loki อีกที
```mermaid
flowchart LR
    subgraph Apps["Applications"]
        A1["APP #1"]
        A2["APP #2"]
        A3["APP #3"]
    end

    subgraph SDK["OTel SDK + eto"]
        B(("Tracing-Metrics-Logs"))
    end

    subgraph Collector["OTel Collector"]
        COL{{"Collector"}}
    end

    subgraph Tracing["Tracing Backends"]
        C1["Jaeger"]
        C2["Tempo"]
    end

    subgraph Metrics["Metrics Backend"]
        M1["Prometheus"]
        M2["Grafana (Dashboards)"]
    end

    subgraph Logging["Logging Backends"]
        L1["sLog (custom sink)"]
        L2["Loki"]
        L3["Zap JSON → stdout"]
    end

    A1 --> B
    A2 --> B
    A3 --> B

    B --> COL

    COL -->|"Traces"| C1
    COL -->|"Traces"| C2

    COL -->|"Metrics"| M1
    M1 --> M2

    COL -->|"Logs"| L1
    COL -->|"Logs"| L2

    B -->|"App Logs (Zap JSON stdout)"| L3
```