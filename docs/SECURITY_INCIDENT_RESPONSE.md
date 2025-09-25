# Security Incident Response Playbook

This document provides detailed procedures for responding to security incidents in MCP Bridge deployments.

## 🚨 **Incident Classification and Response Matrix**

| Severity | Response Time | Team | Escalation | Communication |
|----------|---------------|------|------------|---------------|
| **CRITICAL** | 15 minutes | All hands | CISO, CEO | Immediate alert |
| **HIGH** | 1 hour | Security + DevOps | CTO | 2-hour update |
| **MEDIUM** | 4 hours | Security Team | Team Lead | Daily update |
| **LOW** | 24 hours | On-duty Engineer | None | Weekly report |

## 🎯 **Incident Types and Response Procedures**

### **1. Authentication Compromise**

#### **Symptoms**
- High authentication failure rates
- Unusual login patterns
- Multiple failed attempts from single IP
- Invalid token spikes

#### **Immediate Response (0-15 minutes)**
```bash
# 1. Identify compromised accounts
kubectl logs -l app=mcp-gateway | grep "AUTH_FAILURE" | tail -100

# 2. Block suspicious IPs
kubectl patch networkpolicy gateway-ingress --patch '
spec:
  ingress:
  - from:
    - ipBlock:
        cidr: 0.0.0.0/0
        except:
        - <SUSPICIOUS_IP>/32'

# 3. Rotate JWT secrets
kubectl create secret generic mcp-gateway-jwt --from-literal=secret="$(openssl rand -base64 32)" --dry-run=client -o yaml | kubectl apply -f -

# 4. Force reauthentication
kubectl rollout restart deployment/mcp-gateway
```

#### **Investigation (15-60 minutes)**
```bash
# Analyze attack patterns
kubectl logs -l app=mcp-gateway --since=1h | grep "AUTH" | \
  awk '{print $1, $7}' | sort | uniq -c | sort -nr

# Check for credential stuffing
kubectl logs -l app=mcp-gateway | grep "AUTH_FAILURE" | \
  grep -o 'ip=[0-9.]*' | sort | uniq -c | sort -nr

# Review successful authentications during attack
kubectl logs -l app=mcp-gateway | grep "AUTH_SUCCESS" | \
  grep -E "($(date -d '1 hour ago' '+%Y-%m-%d %H'):.*|$(date '+%Y-%m-%d %H'):.*)"
```

#### **Recovery Actions**
- [ ] Revoke all active sessions
- [ ] Implement additional rate limiting
- [ ] Enable MFA if not already active
- [ ] Notify affected users
- [ ] Update WAF rules

### **2. DDoS Attack**

#### **Symptoms**
- High connection rates
- Resource exhaustion
- Service unavailability
- Network saturation

#### **Immediate Response (0-15 minutes)**
```bash
# 1. Enable auto-scaling
kubectl patch hpa mcp-gateway --patch '
spec:
  maxReplicas: 20
  targetCPUUtilizationPercentage: 50'

# 2. Implement emergency rate limiting
kubectl apply -f - <<EOF
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: mcp-gateway-circuit-breaker
spec:
  host: mcp-gateway
  trafficPolicy:
    connectionPool:
      tcp:
        maxConnections: 100
      http:
        http1MaxPendingRequests: 10
        maxRequestsPerConnection: 2
    outlierDetection:
      consecutiveGatewayErrors: 3
      interval: 30s
      baseEjectionTime: 30s
EOF

# 3. Activate DDoS protection
curl -X POST "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/settings/security_level" \
  -H "Authorization: Bearer ${CF_API_TOKEN}" \
  -H "Content-Type: application/json" \
  --data '{"value":"under_attack"}'
```

#### **Investigation (15-60 minutes)**
```bash
# Analyze traffic patterns
kubectl logs -l app=mcp-gateway --since=30m | \
  grep -o 'source_ip=[0-9.]*' | sort | uniq -c | sort -nr | head -20

# Check for distributed sources
kubectl logs -l app=mcp-gateway --since=30m | \
  awk '/source_ip/ {print $0}' | \
  grep -o 'source_ip=[0-9.]*' | \
  cut -d= -f2 | \
  awk -F. '{print $1"."$2"."$3".0/24"}' | \
  sort | uniq -c | sort -nr

# Monitor resource usage
kubectl top pods -l app=mcp-gateway
kubectl top nodes
```

#### **Recovery Actions**
- [ ] Implement geographic blocking
- [ ] Add upstream DDoS protection
- [ ] Tune auto-scaling parameters
- [ ] Review and update rate limits
- [ ] Implement CAPTCHA challenges

### **3. Container Security Breach**

#### **Symptoms**
- Unusual process execution
- Unauthorized file modifications
- Network connections to suspicious IPs
- Resource usage anomalies

#### **Immediate Response (0-15 minutes)**
```bash
# 1. Isolate affected pods
kubectl patch networkpolicy mcp-gateway-isolation --patch '
spec:
  podSelector:
    matchLabels:
      app: mcp-gateway
  policyTypes:
  - Ingress
  - Egress
  egress: []'

# 2. Collect forensic data
kubectl cp mcp-gateway-<pod-id>:/tmp /tmp/forensics-$(date +%s)
kubectl logs mcp-gateway-<pod-id> --previous > /tmp/pod-logs-$(date +%s).log

# 3. Create memory dump (if possible)
kubectl exec mcp-gateway-<pod-id> -- cat /proc/*/maps > /tmp/memory-maps-$(date +%s).txt

# 4. Terminate compromised pods
kubectl delete pod -l app=mcp-gateway,security-status=compromised
```

#### **Investigation (15-60 minutes)**
```bash
# Check for privilege escalation
kubectl exec -it mcp-gateway-<pod-id> -- ps aux | grep -v "mcp-gateway\|pause"

# Review file system changes
kubectl exec -it mcp-gateway-<pod-id> -- find / -type f -newer /proc/1/stat 2>/dev/null

# Analyze network connections
kubectl exec -it mcp-gateway-<pod-id> -- netstat -tupln | grep -v "127.0.0.1\|::::"

# Check for malicious processes
kubectl exec -it mcp-gateway-<pod-id> -- top -n 1 -b | head -20
```

#### **Recovery Actions**
- [ ] Rebuild containers from clean images
- [ ] Scan images for vulnerabilities
- [ ] Review and tighten security policies
- [ ] Implement runtime security monitoring
- [ ] Update container security baselines

### **4. Data Exfiltration**

#### **Symptoms**
- Unusual outbound network traffic
- Large data transfers
- Access to sensitive endpoints
- Suspicious API usage patterns

#### **Immediate Response (0-15 minutes)**
```bash
# 1. Block suspicious outbound traffic
kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: mcp-gateway-egress-restriction
spec:
  podSelector:
    matchLabels:
      app: mcp-gateway
  policyTypes:
  - Egress
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          name: mcp-system
    ports:
    - protocol: TCP
      port: 6379  # Redis only
EOF

# 2. Enable enhanced logging
kubectl patch deployment mcp-gateway --patch '
spec:
  template:
    spec:
      containers:
      - name: gateway
        env:
        - name: LOG_LEVEL
          value: "debug"
        - name: AUDIT_LOG_ENABLED
          value: "true"'

# 3. Take traffic snapshot
kubectl exec -it mcp-gateway-<pod-id> -- tcpdump -i any -w /tmp/traffic-$(date +%s).pcap &
```

#### **Investigation (15-60 minutes)**
```bash
# Analyze API access patterns
kubectl logs -l app=mcp-gateway --since=2h | \
  grep -E "(GET|POST|PUT|DELETE)" | \
  awk '{print $7, $8, $9}' | sort | uniq -c | sort -nr

# Check data volume by endpoint
kubectl logs -l app=mcp-gateway --since=2h | \
  grep "response_size" | \
  awk '{sum+=$NF} END {print "Total bytes:", sum}'

# Identify suspicious user agents
kubectl logs -l app=mcp-gateway --since=2h | \
  grep -o 'user_agent="[^"]*"' | sort | uniq -c | sort -nr

# Review authentication context
kubectl logs -l app=mcp-gateway --since=2h | \
  grep "DATA_ACCESS" | \
  jq -r '.user_id, .endpoint, .timestamp' | \
  paste - - -
```

#### **Recovery Actions**
- [ ] Revoke compromised credentials
- [ ] Implement data loss prevention (DLP)
- [ ] Enable endpoint monitoring
- [ ] Review and update data access policies
- [ ] Notify data protection authorities if required

## 🔧 **Incident Response Tools**

### **Emergency Response Kit**
```bash
#!/bin/bash
# emergency-response.sh
# Quick incident response toolkit

NAMESPACE="mcp-system"
INCIDENT_ID="INC-$(date +%Y%m%d-%H%M%S)"

echo "🚨 MCP Bridge Emergency Response Kit"
echo "Incident ID: $INCIDENT_ID"

# Create incident directory
mkdir -p "/tmp/incident-$INCIDENT_ID"
cd "/tmp/incident-$INCIDENT_ID"

# Collect system state
echo "📊 Collecting system state..."
kubectl get pods -n $NAMESPACE -o wide > pods-state.txt
kubectl get services -n $NAMESPACE -o wide > services-state.txt
kubectl get networkpolicies -n $NAMESPACE -o yaml > network-policies.yaml

# Collect logs
echo "📋 Collecting logs..."
kubectl logs -l app=mcp-gateway -n $NAMESPACE --since=1h > gateway-logs.txt
kubectl logs -l app=mcp-router -n $NAMESPACE --since=1h > router-logs.txt

# Collect metrics
echo "📈 Collecting metrics..."
curl -s "http://prometheus:9090/api/v1/query?query=up" > metrics-up.json
curl -s "http://prometheus:9090/api/v1/query?query=rate(mcp_gateway_requests_total[5m])" > metrics-requests.json

# Security-specific data
echo "🔒 Collecting security data..."
kubectl logs -l app=mcp-gateway -n $NAMESPACE | grep -i "auth\|security\|error" > security-events.txt

# System performance
echo "💻 Collecting performance data..."
kubectl top pods -n $NAMESPACE > pod-resources.txt
kubectl top nodes > node-resources.txt

echo "✅ Data collection complete: /tmp/incident-$INCIDENT_ID"
echo "📧 Next: Notify security team and begin analysis"
```

### **Forensic Analysis Tools**
```bash
#!/bin/bash
# forensic-analysis.sh
# Advanced forensic analysis for security incidents

INCIDENT_DIR="/tmp/incident-$1"
cd "$INCIDENT_DIR"

echo "🔍 Starting forensic analysis for incident $1"

# Log analysis
echo "📊 Analyzing access patterns..."
cat gateway-logs.txt | \
  grep -E "HTTP/[0-9.]+ [0-9]+" | \
  awk '{print $1, $7, $9}' | \
  sort | uniq -c | sort -nr > access-patterns.txt

# IP analysis
echo "🌐 Analyzing source IPs..."
cat gateway-logs.txt | \
  grep -o 'source_ip=[0-9.]*' | \
  cut -d= -f2 | \
  sort | uniq -c | sort -nr > ip-analysis.txt

# Timeline reconstruction
echo "⏰ Reconstructing timeline..."
cat gateway-logs.txt | \
  grep -E "(ERROR|WARN|SECURITY)" | \
  sort -k1,2 > incident-timeline.txt

# Generate report
echo "📋 Generating incident report..."
cat > incident-report.md <<EOF
# Security Incident Report: $1

## Timeline
$(head -20 incident-timeline.txt)

## Top Source IPs
$(head -10 ip-analysis.txt)

## Access Patterns
$(head -15 access-patterns.txt)

## System State
- Pods: $(wc -l < pods-state.txt) running
- Network Policies: $(grep -c "kind: NetworkPolicy" network-policies.yaml) active
- Security Events: $(wc -l < security-events.txt) logged

## Recommendations
- [ ] Review and update security policies
- [ ] Implement additional monitoring
- [ ] Update incident response procedures
- [ ] Conduct security training

Generated: $(date)
EOF

echo "✅ Forensic analysis complete: incident-report.md"
```

## 📞 **Communication Procedures**

### **Incident Communication Matrix**

| Stakeholder | Critical | High | Medium | Low |
|-------------|----------|------|--------|-----|
| Security Team | Immediate | 15 min | 1 hour | 4 hours |
| Development Team | 30 min | 1 hour | 4 hours | 24 hours |
| DevOps Team | 15 min | 30 min | 2 hours | 8 hours |
| Management | 1 hour | 4 hours | 24 hours | Weekly |
| Legal Team | 2 hours | 24 hours | N/A | N/A |
| Customers | 4 hours | 24 hours | N/A | N/A |

### **Communication Templates**

#### **Initial Alert**
```
🚨 SECURITY INCIDENT ALERT 🚨

Incident ID: INC-YYYYMMDD-HHMMSS
Severity: [CRITICAL/HIGH/MEDIUM/LOW]
Component: MCP Bridge [Gateway/Router]
Status: ACTIVE

Summary: [Brief description of the incident]
Impact: [Service impact and affected users]
Response: [Current response actions]

Next Update: [Time of next update]
Response Team: [Team members assigned]

War Room: [Slack channel/video link]
```

#### **Status Update**
```
📊 INCIDENT UPDATE - INC-YYYYMMDD-HHMMSS

Time: [Current time]
Status: [INVESTIGATING/MITIGATING/RESOLVED]
Duration: [Time since incident start]

Progress:
✅ [Completed actions]
🔄 [In progress actions]
⏳ [Planned actions]

Impact: [Current service impact]
ETA: [Estimated resolution time]

Next Update: [Time of next update]
```

#### **Resolution Notice**
```
✅ INCIDENT RESOLVED - INC-YYYYMMDD-HHMMSS

Resolution Time: [Date and time]
Total Duration: [Total incident duration]
Root Cause: [Brief root cause description]

Actions Taken:
- [Immediate response actions]
- [Mitigation measures]
- [Preventive measures]

Service Impact:
- [Summary of service impact]
- [Affected users/systems]

Follow-up Actions:
- [ ] Post-incident review scheduled
- [ ] Security improvements planned
- [ ] Documentation updates

Post-Incident Review: [Date and time]
```

## 📋 **Post-Incident Procedures**

### **Post-Incident Review Process**

#### **Timeline: Within 48 hours of resolution**

1. **Data Collection (2 hours)**
   - Gather all incident artifacts
   - Collect timeline reconstruction
   - Document response actions taken
   - Compile metrics and impact data

2. **Root Cause Analysis (4 hours)**
   - Identify primary root cause
   - Map contributing factors
   - Analyze response effectiveness
   - Document lessons learned

3. **Action Planning (2 hours)**
   - Define preventive measures
   - Plan security improvements
   - Update procedures and playbooks
   - Assign ownership and deadlines

### **Post-Incident Review Template**
```markdown
# Post-Incident Review: INC-YYYYMMDD-HHMMSS

## Incident Summary
- **Duration**: [Start time] to [End time]
- **Severity**: [Critical/High/Medium/Low]
- **Impact**: [Service impact description]
- **Root Cause**: [Primary root cause]

## Timeline
| Time | Event | Action Taken |
|------|-------|--------------|
| | | |

## What Went Well
- [Positive aspects of response]
- [Effective tools/procedures]
- [Good communication/coordination]

## What Could Be Improved
- [Areas for improvement]
- [Process gaps identified]
- [Tool/skill deficiencies]

## Action Items
| Action | Owner | Due Date | Priority |
|--------|-------|----------|----------|
| | | | |

## Preventive Measures
- [Technical improvements]
- [Process enhancements]
- [Training needs]

## Lessons Learned
- [Key insights gained]
- [Knowledge to share with team]
- [Industry best practices confirmed]
```

## 🎓 **Training and Preparedness**

### **Regular Incident Response Exercises**

#### **Monthly Tabletop Exercises**
- Authentication compromise scenarios
- DDoS attack simulations
- Data breach response
- Container security incidents

#### **Quarterly Red Team Exercises**
- Penetration testing scenarios
- Social engineering simulations
- Physical security assessments
- Supply chain attack simulations

#### **Annual Disaster Recovery Tests**
- Full system compromise recovery
- Data center failover procedures
- Communication system failures
- Long-term incident management

### **Training Requirements**

#### **All Team Members**
- [ ] Security awareness fundamentals
- [ ] Incident identification and reporting
- [ ] Communication procedures
- [ ] Basic forensic preservation

#### **Security Team**
- [ ] Advanced incident response
- [ ] Digital forensics techniques
- [ ] Threat hunting methodologies
- [ ] Malware analysis basics

#### **Development Team**
- [ ] Secure coding practices
- [ ] Security tool usage
- [ ] Code review for security
- [ ] Vulnerability remediation

#### **Operations Team**
- [ ] Security monitoring
- [ ] Log analysis techniques
- [ ] Infrastructure hardening
- [ ] Backup and recovery procedures

## 📊 **Incident Metrics and KPIs**

### **Response Time Metrics**
- Mean Time to Detection (MTTD)
- Mean Time to Response (MTTR)
- Mean Time to Resolution (MTTR)
- Mean Time Between Incidents (MTBI)

### **Quality Metrics**
- Incident recurrence rate
- False positive rate
- Escalation accuracy
- Customer satisfaction score

### **Process Metrics**
- Playbook adherence rate
- Training completion rate
- Exercise participation rate
- Action item completion rate

---

**This incident response playbook is a living document that should be updated based on lessons learned from actual incidents and regular exercise outcomes.**

**Document Classification**: Internal Use  
**Last Updated**: August 2025  
**Next Review**: November 2025  
**Owner**: Security Team