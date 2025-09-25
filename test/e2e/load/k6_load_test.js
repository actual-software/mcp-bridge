import ws from 'k6/ws';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { randomString, randomIntBetween } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';

// Custom metrics
const connectionSuccess = new Rate('connection_success');
const requestSuccess = new Rate('request_success');
const requestDuration = new Trend('request_duration');
const connectionErrors = new Counter('connection_errors');
const requestErrors = new Counter('request_errors');
const rateLimitErrors = new Counter('rate_limit_errors');

// Test configuration
export const options = {
  scenarios: {
    // Scenario 1: Ramp up to 10k concurrent connections
    concurrent_connections: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '2m', target: 1000 },   // Ramp up to 1k users
        { duration: '3m', target: 5000 },   // Ramp up to 5k users
        { duration: '5m', target: 10000 },  // Ramp up to 10k users
        { duration: '10m', target: 10000 }, // Stay at 10k for 10 minutes
        { duration: '5m', target: 0 },      // Ramp down
      ],
      gracefulRampDown: '30s',
    },
    
    // Scenario 2: Burst traffic test
    burst_traffic: {
      executor: 'ramping-arrival-rate',
      startTime: '25m',
      preAllocatedVUs: 2000,
      maxVUs: 5000,
      stages: [
        { duration: '30s', target: 100 },   // Normal traffic
        { duration: '10s', target: 1000 },  // Sudden burst
        { duration: '30s', target: 1000 },  // Sustained burst
        { duration: '10s', target: 100 },   // Back to normal
      ],
    },
    
    // Scenario 3: Sustained load test
    sustained_load: {
      executor: 'constant-vus',
      vus: 1000,
      duration: '30m',
      startTime: '35m',
    },
  },
  
  thresholds: {
    'connection_success': ['rate>0.95'],      // 95% connection success rate
    'request_success': ['rate>0.99'],         // 99% request success rate
    'request_duration': ['p(95)<100'],        // 95% of requests under 100ms
    'request_duration': ['p(99)<500'],        // 99% of requests under 500ms
    'http_req_duration': ['p(95)<200'],       // HTTP handshake under 200ms
  },
};

// Configuration
const GATEWAY_URL = __ENV.GATEWAY_URL || 'wss://localhost:8443/ws';
const AUTH_TOKEN = __ENV.AUTH_TOKEN || generateTestToken();
const REQUEST_RATE = __ENV.REQUEST_RATE || 10; // Requests per second per connection

export default function () {
  const url = GATEWAY_URL;
  const params = {
    headers: {
      'Authorization': `Bearer ${AUTH_TOKEN}`,
    },
    tags: { scenario: __ENV.scenario },
  };

  // Establish WebSocket connection
  const response = ws.connect(url, params, function (socket) {
    // Connection successful
    connectionSuccess.add(true);
    
    // Set up message handler
    socket.on('message', function (data) {
      try {
        const response = JSON.parse(data);
        const duration = Date.now() - parseInt(response.id.split('-')[2]);
        
        if (response.error) {
          requestSuccess.add(false);
          requestErrors.add(1);
          
          // Check for rate limiting
          if (response.error.code === -32029) {
            rateLimitErrors.add(1);
          }
        } else {
          requestSuccess.add(true);
          requestDuration.add(duration);
        }
      } catch (e) {
        requestErrors.add(1);
      }
    });

    socket.on('error', function (e) {
      connectionErrors.add(1);
    });

    // Send requests at specified rate
    const requestInterval = 1000 / REQUEST_RATE;
    let requestId = 0;
    
    socket.setInterval(function () {
      const request = {
        jsonrpc: '2.0',
        method: getRandomMethod(),
        params: {
          vu_id: __VU,
          iteration: __ITER,
          timestamp: Date.now(),
          random: randomIntBetween(1, 1000),
        },
        id: `k6-${__VU}-${Date.now()}-${requestId++}`,
      };
      
      socket.send(JSON.stringify(request));
    }, requestInterval);

    // Keep connection alive for test duration
    socket.setTimeout(function () {
      socket.close();
    }, 300000); // 5 minutes max per connection
  });

  // Check connection result
  check(response, {
    'Connected successfully': (r) => r && r.status === 101,
  });

  if (!response || response.status !== 101) {
    connectionSuccess.add(false);
    connectionErrors.add(1);
  }
}

// Helper function to generate test methods
function getRandomMethod() {
  const methods = [
    'tools/list',
    'resources/list',
    'prompts/list',
    'server/info',
    'completion/create',
    'resources/read',
    'tools/call',
  ];
  return methods[randomIntBetween(0, methods.length - 1)];
}

// Generate a test JWT token
function generateTestToken() {
  // In a real test, this would generate a proper JWT
  return 'test-token-' + randomString(32);
}

// Custom summary report
export function handleSummary(data) {
  return {
    'stdout': textSummary(data, { indent: ' ', enableColors: true }),
    'load-test-results.json': JSON.stringify(data, null, 2),
    'load-test-summary.html': htmlReport(data),
  };
}

// Generate HTML report
function htmlReport(data) {
  const scenarios = data.root_group.groups;
  const metrics = data.metrics;
  
  return `
<!DOCTYPE html>
<html>
<head>
    <title>MCP Gateway Load Test Results</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .metric { margin: 10px 0; padding: 10px; background: #f0f0f0; }
        .success { color: green; }
        .failure { color: red; }
        table { border-collapse: collapse; width: 100%; }
        th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
        th { background-color: #4CAF50; color: white; }
    </style>
</head>
<body>
    <h1>MCP Gateway Load Test Results</h1>
    
    <h2>Summary</h2>
    <div class="metric">
        <strong>Total Duration:</strong> ${data.state.testRunDurationMs}ms
    </div>
    <div class="metric">
        <strong>Total VUs:</strong> ${data.state.vus}
    </div>
    
    <h2>Connection Metrics</h2>
    <div class="metric">
        <strong>Connection Success Rate:</strong> 
        <span class="${metrics.connection_success.rate > 0.95 ? 'success' : 'failure'}">
            ${(metrics.connection_success.rate * 100).toFixed(2)}%
        </span>
    </div>
    <div class="metric">
        <strong>Total Connection Errors:</strong> ${metrics.connection_errors.count}
    </div>
    
    <h2>Request Metrics</h2>
    <div class="metric">
        <strong>Request Success Rate:</strong> 
        <span class="${metrics.request_success.rate > 0.99 ? 'success' : 'failure'}">
            ${(metrics.request_success.rate * 100).toFixed(2)}%
        </span>
    </div>
    <div class="metric">
        <strong>Total Requests:</strong> ${metrics.request_success.count + metrics.request_errors.count}
    </div>
    <div class="metric">
        <strong>Rate Limit Errors:</strong> ${metrics.rate_limit_errors.count}
    </div>
    
    <h2>Performance Metrics</h2>
    <table>
        <tr>
            <th>Metric</th>
            <th>Min</th>
            <th>Median</th>
            <th>P95</th>
            <th>P99</th>
            <th>Max</th>
        </tr>
        <tr>
            <td>Request Duration (ms)</td>
            <td>${metrics.request_duration.min.toFixed(2)}</td>
            <td>${metrics.request_duration.med.toFixed(2)}</td>
            <td>${metrics.request_duration['p(95)'].toFixed(2)}</td>
            <td>${metrics.request_duration['p(99)'].toFixed(2)}</td>
            <td>${metrics.request_duration.max.toFixed(2)}</td>
        </tr>
    </table>
    
    <h2>Scenario Results</h2>
    ${Object.entries(scenarios).map(([name, scenario]) => `
        <h3>${name}</h3>
        <div class="metric">
            <strong>Duration:</strong> ${scenario.duration}ms
        </div>
        <div class="metric">
            <strong>Iterations:</strong> ${scenario.iterations}
        </div>
    `).join('')}
</body>
</html>
  `;
}

// Memory leak detection scenario
export function memoryLeakTest() {
  const url = GATEWAY_URL;
  const params = {
    headers: {
      'Authorization': `Bearer ${AUTH_TOKEN}`,
    },
  };

  // Create connection that intentionally doesn't clean up properly
  const response = ws.connect(url, params, function (socket) {
    // Create large objects that might not be garbage collected
    let leakyArray = [];
    
    socket.on('message', function (data) {
      // Intentionally store all messages to simulate memory leak
      leakyArray.push(JSON.parse(data));
      
      // Also create circular references
      const obj = { data: data };
      obj.self = obj;
      leakyArray.push(obj);
    });

    // Send requests continuously
    socket.setInterval(function () {
      const largePayload = {
        jsonrpc: '2.0',
        method: 'test/memory',
        params: {
          data: randomString(10000), // 10KB payload
          nested: {
            level1: { level2: { level3: { data: randomString(5000) } } }
          }
        },
        id: `leak-${__VU}-${Date.now()}`,
      };
      
      socket.send(JSON.stringify(largePayload));
    }, 100); // High frequency
  });
}

// Run memory leak test as separate scenario
export const memoryLeakOptions = {
  scenarios: {
    memory_leak_detection: {
      executor: 'constant-vus',
      vus: 100,
      duration: '48h', // 48-hour test
      exec: 'memoryLeakTest',
    },
  },
  thresholds: {
    'ws_session_duration': ['avg>3600000'], // Sessions should last > 1 hour
  },
};