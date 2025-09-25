# MCP Bridge Documentation Strategy
*Based on Implementation Status Report - 2025-09-05*

## Core Principles

### 1. Truth Over Aspiration
- Document ONLY features that pass tests
- Remove all "coming soon" or "planned" features
- Mark experimental features with clear warnings
- Version everything accurately (beta, not rc1)

### 2. Evidence-Based Documentation
- Every feature claim must reference working code
- Include test status for each documented feature
- Link to specific implementation files
- Show actual configuration examples that work

### 3. User-Centric Structure
- Start with what works reliably
- Clear warnings for experimental features
- Migration paths for breaking changes
- Troubleshooting based on actual issues

## Documentation Taxonomy

### Level 1: Core Documentation (Stable Features)
**Priority: IMMEDIATE**

#### Files to Update:
1. **README.md**
   - Current version (beta, not v1.0.0-rc1)
   - Remove "Protocol Auto-Detection: 98% accuracy" (tests fail)
   - Focus on WebSocket as primary protocol
   - Add "Experimental Features" section

2. **docs/QUICKSTART.md**
   - WebSocket-first examples
   - Remove stdio direct connection examples
   - Add health check verification steps
   - Include working configuration only

3. **docs/api.md**
   - Document WebSocket API (working)
   - Mark HTTP as "partial support"
   - Remove SSE examples until stable
   - Add error codes from actual tests

### Level 2: Architecture Documentation
**Priority: HIGH**

#### Files to Create/Update:
1. **docs/CURRENT_STATE.md** (NEW)
   ```markdown
   # Current Implementation State
   ## Working Features
   - WebSocket Gateway (Full support)
   - HTTP Client (Basic operations)
   - Connection Pooling (Stable)
   - Memory Optimization (Stable)
   
   ## Experimental Features
   - Direct stdio connections (Tests failing)
   - SSE Client (Partial implementation)
   - Adaptive Timeout (Known issues)
   
   ## Not Implemented
   - Protocol auto-detection (Claims 98%, actually fails)
   - Full SSE streaming
   ```

2. **docs/architecture.md**
   - Update diagrams to show actual implementation
   - Remove references to non-working features
   - Add "Implementation Status" badges

### Level 3: Configuration Documentation
**Priority: HIGH**

1. **docs/configuration.md**
   - Only document working options
   - Mark experimental settings
   - Provide tested examples
   
2. **Example Configs** (Update)
   ```yaml
   # mcp-router-stable.yaml
   gateway:
     protocol: websocket  # STABLE
     url: wss://gateway:8443
   
   # Experimental - may not work
   # direct:
   #   stdio:  # FAILING TESTS
   ```

### Level 4: API & Protocol Documentation
**Priority: MEDIUM**

1. **services/router/docs/API.md**
   - Current actual endpoints
   - Real error codes from tests
   - Working request/response examples

2. **services/gateway/docs/BINARY_PROTOCOL.md**
   - Mark as "Partial Implementation"
   - Document what actually works
   - Reference test files

### Level 5: Developer Documentation
**Priority: MEDIUM**

1. **CONTRIBUTING.md** (Update)
   - Current test status
   - How to run working tests
   - Known failing tests to avoid

2. **docs/development/testing.md**
   - Actual test commands that work
   - Expected failures
   - Test coverage reality

## Implementation Plan

### Phase 1: Truth Reconciliation (Week 1)
- [ ] Update version references (v1.0.0-rc1 → beta)
- [ ] Remove/mark non-working features
- [ ] Create CURRENT_STATE.md
- [ ] Update README.md with reality

### Phase 2: Core Docs Update (Week 2)
- [ ] Revise QUICKSTART.md for working features
- [ ] Update configuration.md
- [ ] Fix API documentation
- [ ] Create working examples

### Phase 3: Architecture Alignment (Week 3)
- [ ] Update architecture diagrams
- [ ] Document actual data flows
- [ ] Create sequence diagrams for working protocols
- [ ] Add implementation notes

### Phase 4: Migration & Troubleshooting (Week 4)
- [ ] Create MIGRATION.md for version changes
- [ ] Update troubleshooting.md with real issues
- [ ] Document known limitations
- [ ] Add workarounds

## Documentation Standards

### Feature Documentation Template
```markdown
## [Feature Name]

**Status**: 🟢 Stable | 🟡 Experimental | 🔴 Not Working
**Test Coverage**: X/Y tests passing
**Implementation**: `path/to/implementation.go`

### Description
[What it does when working]

### Configuration
[Working configuration example]

### Known Issues
- [Issue 1]: [Workaround if any]
- [Issue 2]: [Link to tracking issue]

### Example Usage
[Actual working example]
```

### Configuration Documentation Rules
1. Every config option must be tested
2. Include default values
3. Mark required vs optional
4. Show real-world example
5. Link to implementation

### Error Documentation Format
```markdown
## Error Code: [CODE]
**Component**: [Where it occurs]
**Test File**: [Link to test showing error]
**Meaning**: [What went wrong]
**Resolution**: [How to fix]
```

## Validation Checklist

Before publishing ANY documentation:

- [ ] Feature has passing tests
- [ ] Configuration example tested
- [ ] Error codes verified in code
- [ ] File paths accurate
- [ ] Version numbers correct
- [ ] No placeholder content
- [ ] No "coming soon" features
- [ ] Experimental features marked
- [ ] Implementation files linked

## Metrics for Success

1. **Accuracy**: 100% of documented features work
2. **Coverage**: 100% of working features documented
3. **Clarity**: 0 user issues due to incorrect docs
4. **Currency**: Docs updated within 1 week of changes

## Review Process

1. **Technical Review**: Developer validates implementation
2. **Test Review**: QA confirms test status
3. **User Review**: Test with fresh environment
4. **Final Review**: No aspirational content

## Documentation Debt Tracking

### Critical Issues
1. Version mismatch (rc1 vs beta)
2. Non-working features documented
3. Missing implementation reality

### Track in Issues
- Tag: `documentation-debt`
- Priority: Based on user impact
- Link to implementation status

## Maintenance Schedule

- **Weekly**: Update test status
- **Bi-weekly**: Review configuration changes  
- **Monthly**: Full accuracy audit
- **Quarterly**: Architecture review

## Conclusion

The documentation must be radically honest about the current state while providing clear paths forward. Users should know exactly what works, what's experimental, and what's planned. This builds trust and reduces support burden.

### Next Steps
1. Create IMPLEMENTATION_STATUS.md ✅
2. Review with team
3. Begin Phase 1 updates
4. Set up documentation CI/CD checks