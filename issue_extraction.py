#!/usr/bin/env python3
"""
Issue Extraction and Batching Script
Extracts individual linting issues from audit results and categorizes by priority
"""

import re
import json
from collections import defaultdict
from pathlib import Path

def extract_linting_issues(lint_results_file):
    """Extract individual linting issues from audit results"""
    issues = []
    issue_counter = 0
    
    # Priority mapping
    priority_map = {
        'gosec': 1,        # Security - Critical
        'errcheck': 2,     # Error handling - High  
        'goimports': 3,    # Import issues - High
        'gofmt': 4,        # Formatting - Medium
        'wsl_v5': 5,       # Style - Medium
        'godot': 5,        # Style - Medium  
        'nlreturn': 5,     # Style - Medium
        'perfsprint': 6,   # Performance - Low
        'prealloc': 6,     # Performance - Low
    }
    
    with open(lint_results_file, 'r') as f:
        content = f.read()
    
    # Skip problematic modules with syntax errors
    skip_modules = ['test/e2e/k8s', 'test/e2e/weather', 'test/e2e/full_stack']
    
    # Pattern to match linting issues: file:line:column: description (linter)
    issue_pattern = r'^([^:]+):(\d+):(\d+):\s+(.+)\s+\(([^)]+)\)$'
    
    lines = content.split('\n')
    current_module = None
    
    for line in lines:
        # Track current module
        if line.startswith('=== LINTING MODULE:'):
            current_module = line.split(':')[1].strip()
            continue
            
        # Skip issues from problematic modules
        if any(skip_mod in line for skip_mod in skip_modules):
            continue
            
        # Extract linting issue
        match = re.match(issue_pattern, line.strip())
        if match:
            file_path, line_num, col_num, description, linter = match.groups()
            
            # Skip typecheck errors (syntax errors)
            if linter == 'typecheck':
                continue
                
            # Only process issues from services modules (main fixable issues)
            if not file_path.startswith('services/'):
                continue
                
            issue_counter += 1
            priority = priority_map.get(linter, 7)  # Default low priority for unknown linters
            
            issue = {
                'id': f'issue_{issue_counter:04d}',
                'file_path': file_path,
                'line_number': int(line_num),
                'column_number': int(col_num),
                'linter_name': linter,
                'description': description.strip(),
                'priority': priority,
                'module_path': current_module or 'unknown',
                'status': 'pending'
            }
            issues.append(issue)
    
    return issues

def categorize_by_priority(issues):
    """Categorize issues by priority for batch processing"""
    priority_groups = defaultdict(list)
    
    for issue in issues:
        priority_groups[issue['priority']].append(issue)
    
    return priority_groups

def create_batches(issues, batch_size=10):
    """Create batches of issues for parallel processing"""
    batches = []
    
    for i in range(0, len(issues), batch_size):
        batch = issues[i:i + batch_size]
        batch_id = f'batch_{len(batches) + 1:03d}'
        batches.append({
            'batch_id': batch_id,
            'issues': batch,
            'size': len(batch),
            'status': 'pending'
        })
    
    return batches

def main():
    lint_results_file = Path('audit-results/lint-results-20250820-011914.txt')
    
    if not lint_results_file.exists():
        print(f"❌ Lint results file not found: {lint_results_file}")
        return
    
    print("🔍 EXTRACTING LINTING ISSUES")
    print("=" * 50)
    
    # Extract all issues
    issues = extract_linting_issues(lint_results_file)
    print(f"📊 Total fixable issues extracted: {len(issues)}")
    
    # Categorize by priority
    priority_groups = categorize_by_priority(issues)
    
    print("\n📈 ISSUES BY PRIORITY:")
    priority_names = {1: 'Critical (Security)', 2: 'High (Errors)', 3: 'High (Imports)', 
                     4: 'Medium (Format)', 5: 'Medium (Style)', 6: 'Low (Performance)'}
    
    for priority in sorted(priority_groups.keys()):
        count = len(priority_groups[priority])
        name = priority_names.get(priority, f'Priority {priority}')
        print(f"  {priority}: {name} - {count} issues")
    
    # Create processing order (priority 1 first, then 2, etc.)
    ordered_issues = []
    for priority in sorted(priority_groups.keys()):
        ordered_issues.extend(priority_groups[priority])
    
    # Create batches  
    batches = create_batches(ordered_issues, batch_size=10)
    print(f"\n🔧 Created {len(batches)} batches for parallel processing")
    
    # Save results
    output_data = {
        'summary': {
            'total_issues': len(issues),
            'total_batches': len(batches),
            'priority_breakdown': {str(p): len(issues) for p, issues in priority_groups.items()}
        },
        'batches': batches
    }
    
    output_file = Path('audit-results/extracted_issues.json')
    with open(output_file, 'w') as f:
        json.dump(output_data, f, indent=2)
    
    print(f"💾 Issues saved to: {output_file}")
    
    # Show first few batches for preview
    print(f"\n📋 BATCH PREVIEW (First 3 batches):")
    for i, batch in enumerate(batches[:3]):
        print(f"\n  Batch {batch['batch_id']}: {batch['size']} issues")
        for j, issue in enumerate(batch['issues'][:3]):  # Show first 3 issues per batch
            print(f"    {j+1}. {issue['linter_name']}: {issue['file_path']}:{issue['line_number']}")
        if len(batch['issues']) > 3:
            print(f"    ... and {len(batch['issues']) - 3} more")

if __name__ == '__main__':
    main()