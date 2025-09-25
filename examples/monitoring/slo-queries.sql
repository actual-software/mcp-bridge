-- Example SQL queries for SLO reporting and analysis
-- These queries can be used with Prometheus data exported to a SQL database

-- Monthly SLO Performance Summary
SELECT 
  service,
  slo_name,
  AVG(sli_value) as avg_sli,
  MIN(sli_value) as min_sli,
  PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY sli_value) as p95_sli,
  slo_target,
  COUNT(CASE WHEN sli_value < slo_target THEN 1 END) as violations,
  COUNT(*) as total_measurements,
  (COUNT(CASE WHEN sli_value >= slo_target THEN 1 END)::FLOAT / COUNT(*)) as success_rate
FROM slo_measurements
WHERE timestamp >= NOW() - INTERVAL '30 days'
GROUP BY service, slo_name, slo_target
ORDER BY service, slo_name;

-- Error Budget Consumption Trend
SELECT 
  date_trunc('day', timestamp) as day,
  service,
  slo_name,
  1 - AVG(sli_value) as error_rate,
  (1 - slo_target) as error_budget,
  ((1 - AVG(sli_value)) / (1 - slo_target)) as budget_consumption_rate
FROM slo_measurements
WHERE timestamp >= NOW() - INTERVAL '30 days'
GROUP BY day, service, slo_name, slo_target
ORDER BY day DESC, service;

-- Weekly SLO Status Report
SELECT 
  date_trunc('week', timestamp) as week,
  service,
  slo_name,
  COUNT(*) as total_measurements,
  COUNT(CASE WHEN sli_value >= slo_target THEN 1 END) as successful_measurements,
  AVG(sli_value) as avg_sli,
  MIN(sli_value) as worst_sli,
  MAX(sli_value) as best_sli,
  STDDEV(sli_value) as sli_stddev,
  slo_target
FROM slo_measurements
WHERE timestamp >= NOW() - INTERVAL '8 weeks'
GROUP BY week, service, slo_name, slo_target
ORDER BY week DESC, service, slo_name;

-- Burn Rate Analysis
WITH hourly_errors AS (
  SELECT 
    date_trunc('hour', timestamp) as hour,
    service,
    slo_name,
    1 - AVG(sli_value) as error_rate,
    (1 - MAX(slo_target)) as allowed_error_rate
  FROM slo_measurements
  WHERE timestamp >= NOW() - INTERVAL '7 days'
  GROUP BY hour, service, slo_name
)
SELECT 
  service,
  slo_name,
  AVG(error_rate / NULLIF(allowed_error_rate, 0)) as avg_burn_rate,
  MAX(error_rate / NULLIF(allowed_error_rate, 0)) as max_burn_rate,
  COUNT(CASE WHEN error_rate / NULLIF(allowed_error_rate, 0) > 14.4 THEN 1 END) as critical_burn_hours,
  COUNT(CASE WHEN error_rate / NULLIF(allowed_error_rate, 0) > 3 THEN 1 END) as warning_burn_hours
FROM hourly_errors
GROUP BY service, slo_name
ORDER BY max_burn_rate DESC;

-- SLO Violation Details
SELECT 
  timestamp,
  service,
  slo_name,
  sli_value,
  slo_target,
  (slo_target - sli_value) as violation_margin,
  error_details
FROM slo_measurements
WHERE sli_value < slo_target
  AND timestamp >= NOW() - INTERVAL '24 hours'
ORDER BY timestamp DESC
LIMIT 100;

-- Service Comparison Dashboard
SELECT 
  service,
  COUNT(DISTINCT slo_name) as slo_count,
  AVG(CASE WHEN slo_name = 'availability' THEN sli_value END) as avg_availability,
  AVG(CASE WHEN slo_name = 'latency_p95' THEN sli_value END) as avg_latency_compliance,
  AVG(CASE WHEN slo_name = 'success_rate' THEN sli_value END) as avg_success_rate,
  MIN(sli_value) as worst_sli_any_slo,
  COUNT(CASE WHEN sli_value < slo_target THEN 1 END) as total_violations
FROM slo_measurements
WHERE timestamp >= NOW() - INTERVAL '7 days'
GROUP BY service
ORDER BY total_violations DESC, service;

-- Error Budget Status by Service
WITH current_performance AS (
  SELECT 
    service,
    slo_name,
    AVG(sli_value) as current_sli,
    MAX(slo_target) as slo_target
  FROM slo_measurements
  WHERE timestamp >= NOW() - INTERVAL '28 days'
  GROUP BY service, slo_name
)
SELECT 
  service,
  slo_name,
  current_sli,
  slo_target,
  CASE 
    WHEN current_sli >= slo_target THEN 1.0
    WHEN (1 - current_sli) >= (1 - slo_target) THEN 0.0
    ELSE 1 - ((1 - current_sli) / (1 - slo_target))
  END as error_budget_remaining,
  CASE 
    WHEN current_sli >= slo_target THEN 'Healthy'
    WHEN (1 - current_sli) >= (1 - slo_target) THEN 'Exhausted'
    WHEN 1 - ((1 - current_sli) / (1 - slo_target)) < 0.2 THEN 'Critical'
    WHEN 1 - ((1 - current_sli) / (1 - slo_target)) < 0.5 THEN 'Warning'
    ELSE 'Good'
  END as budget_status
FROM current_performance
ORDER BY error_budget_remaining ASC, service, slo_name;

-- Time to Error Budget Exhaustion
WITH daily_burn AS (
  SELECT 
    service,
    slo_name,
    AVG(1 - sli_value) as avg_daily_error_rate,
    MAX(1 - slo_target) as allowed_monthly_error_rate
  FROM slo_measurements
  WHERE timestamp >= NOW() - INTERVAL '7 days'
  GROUP BY service, slo_name
),
current_budget AS (
  SELECT 
    service,
    slo_name,
    CASE 
      WHEN AVG(sli_value) >= MAX(slo_target) THEN 1.0
      ELSE GREATEST(0, 1 - ((1 - AVG(sli_value)) / (1 - MAX(slo_target))))
    END as budget_remaining
  FROM slo_measurements
  WHERE timestamp >= NOW() - INTERVAL '28 days'
  GROUP BY service, slo_name
)
SELECT 
  d.service,
  d.slo_name,
  b.budget_remaining,
  d.avg_daily_error_rate,
  d.allowed_monthly_error_rate,
  CASE 
    WHEN d.avg_daily_error_rate = 0 THEN 'Never'
    WHEN b.budget_remaining <= 0 THEN 'Already Exhausted'
    ELSE (b.budget_remaining * d.allowed_monthly_error_rate / d.avg_daily_error_rate)::INT || ' days'
  END as time_to_exhaustion
FROM daily_burn d
JOIN current_budget b ON d.service = b.service AND d.slo_name = b.slo_name
WHERE d.avg_daily_error_rate > 0
ORDER BY 
  CASE 
    WHEN b.budget_remaining <= 0 THEN 0
    ELSE b.budget_remaining * d.allowed_monthly_error_rate / d.avg_daily_error_rate
  END ASC;