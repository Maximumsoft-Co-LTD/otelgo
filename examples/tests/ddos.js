import http from 'k6/http'
import { check, sleep } from 'k6'
import { Rate, Trend, Counter } from 'k6/metrics'

const BASE_URL = __ENV.BASE_URL || 'http://127.0.0.1:8091/hello'
const TOKEN    = __ENV.TOKEN     || ''
const HEADERS  = TOKEN ? { 'Authorization': `Bearer ${TOKEN}`, 'Content-Type': 'application/json' } : { 'Content-Type': 'application/json' }

export const errors = new Rate('errors')
export const t_all  = new Trend('latency_all', true)
export const rc_5xx = new Counter('http_5xx')

/**
 * เปิดหลาย scenario เพื่อ “ยิงเยอะขึ้น”
 * - big_ramp_vus: ขยาย VUs ถึง 2k
 * - car_high_rps: ยิงตามอัตรา (arrival rate) ถึง 3k RPS
 * - spike: บูสท์สั้นๆ 10k RPS
 * - soak: ยิงยาวคงที่ 200 RPS นาน 30 นาที
 */
export const options = {
    scenarios: {
        big_ramp_vus: {
            executor: 'ramping-vus',
            startVUs: 0,
            stages: [
                { duration: '1m', target: 200 },
                { duration: '2m', target: 800 },
                { duration: '3m', target: 2000 },   // ปรับตาม backend ไหว
                { duration: '2m', target: 2000 },
                { duration: '1m', target: 0 },
            ],
            gracefulRampDown: '30s',
            exec: 'scenarioVUs',
            tags: { scenario: 'ramp_vus' },
        },

        car_high_rps: {
            executor: 'constant-arrival-rate',
            rate: 1500,                  // เริ่มที่ 1.5k RPS
            timeUnit: '1s',
            duration: '2m',
            preAllocatedVUs: 1000,       // กันคิวรัน
            maxVUs: 4000,                // อนุญาต VUs ขยายตามต้องการ
            exec: 'scenarioCAR',
            tags: { scenario: 'car_rps_1500' },
        },

        spike: {
            executor: 'externally-controlled', // ใช้ k6 pause/resume/scale ได้
            vus: 0,
            maxVUs: 12000,               // เผื่อ spike สูงมาก
            duration: '5m',
            exec: 'scenarioSpike',
            tags: { scenario: 'spike' },
            // เริ่มต้นด้วย 0; ใช้ k6 cloud/ctl scale ขึ้นระหว่างรัน
        },

        soak: {
            executor: 'constant-arrival-rate',
            rate: 200,                   // 200 RPS ยาวๆ
            timeUnit: '1s',
            duration: '30m',
            preAllocatedVUs: 400,
            maxVUs: 1000,
            exec: 'scenarioCAR',
            tags: { scenario: 'soak' },
            startTime: '10m',            // เริ่มหลังอีก scenarios เพื่อไม่ชนกัน
        },
    },

    thresholds: {
        errors: ['rate<0.01'],                  // error rate < 1%
        http_req_failed: ['rate<0.01'],         // network/request fail < 1%
        http_req_duration: ['p(95)<800','p(99)<1500'], // latency
        'http_5xx': ['count==0'],               // ไม่ควรมี 5xx
    },

    // เพิ่ม connections พร้อมกัน
    insecureSkipTLSVerify: true,              // ปิดถ้า prod TLS ถูกต้อง
}

/* ========== Workloads ========== */

// ชุด VUs ปกติ (ผสม GET/POST)
export function scenarioVUs() {
    hitList()
    // hitCreate()
    sleep(0.2 + Math.random() * 0.8) // think-time
}

// ยิงแบบ CAR: เน้น endpoint เดียว/ไม่ sleep เพื่อรักษา RPS
export function scenarioCAR() {
    hitList() // โฟกัสที่ read-heavy
}

// spike workload: ยิงหลาย endpoint เร็วๆ
export function scenarioSpike() {
    if (__ITER % 5 === 0) {
        // hitCreate()
    } else {
        hitList()
    }
}

/* ========== Endpoints ========== */
function hitList() {
    const url = `${BASE_URL}`
    const res = http.get(url, { headers: HEADERS })
    record(res)
    check(res, {
        'GET 200': (r) => r.status === 200,
        // 'has items': (r) => r.json('items') !== undefined,
    }) || errors.add(1)
}