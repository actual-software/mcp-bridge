#!/bin/bash

# Automated WSL_V5 fixer - focused on Go files only

set -e
cd /Users/poile/repos/mcp

echo "🚀 AUTOMATED WSL_V5 ELIMINATION STARTING..."

# Get exact count
before_count=$(golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
echo "📊 TARGET: $before_count WSL_V5 VIOLATIONS"

if [ "$before_count" -eq 0 ]; then
    echo "🎉 MISSION ALREADY COMPLETE!"
    exit 0
fi

# Get Go files with violations only
go_violation_files=$(golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "\.go:" | cut -d: -f1 | sort -u)

if [ -z "$go_violation_files" ]; then
    echo "❌ NO GO FILES WITH VIOLATIONS FOUND"
    exit 1
fi

echo "🎯 TARGETING $(echo "$go_violation_files" | wc -l | tr -d ' ') GO FILES"

files_fixed=0

for file in $go_violation_files; do
    if [[ "$file" == *.go ]] && [ -f "$file" ]; then
        echo "🔧 $file"
        
        temp_file="${file}.fix_tmp"
        
        # Single-pass fix with all patterns
        python3 << EOF
import re

with open("$file", 'r') as f:
    lines = f.readlines()

fixed_lines = []
i = 0
while i < len(lines):
    current = lines[i].rstrip()
    prev = lines[i-1].rstrip() if i > 0 else ""
    prev_prev = lines[i-2].rstrip() if i > 1 else ""
    
    # Pattern 1: Add blank line after require.Error() before if statements
    if (re.match(r'\s+if.*!= ""', current) and 
        re.search(r'require\.Error\(t, err\)', prev) and
        not re.match(r'^\s*$', prev)):
        fixed_lines.append("")
    
    # Pattern 2: Add blank line after t.Parallel() before assignments
    if (re.match(r'\s*\w+.*[=:]', current) and 
        re.search(r't\.Parallel\(\)', prev) and
        not re.match(r'^\s*$', prev)):
        fixed_lines.append("")
    
    # Pattern 3: Add blank line before return statements
    if (re.match(r'\s*return ', current) and
        not re.match(r'^\s*$', prev) and prev and
        not re.match(r'^\s*[{}]', prev) and
        not re.match(r'^\s*$', prev_prev)):
        fixed_lines.append("")
        
    # Pattern 4: Add blank line before range/defer/go
    if (re.match(r'\s*(for.*range|defer |go )', current) and
        not re.match(r'^\s*$', prev) and prev and
        not re.match(r'^\s*{', prev)):
        fixed_lines.append("")
    
    # Pattern 5: Add blank line before assertions after statements  
    if (re.match(r'\s*(assert\.|require\.|t\.)', current) and
        not re.match(r'^\s*$', prev) and prev and
        re.search(r'[;})\]]$', prev) and
        not re.search(r'(assert\.|require\.|t\.)', prev)):
        fixed_lines.append("")
    
    fixed_lines.append(current)
    i += 1

# Remove consecutive blank lines
final_lines = []
prev_blank = False
for line in fixed_lines:
    is_blank = not line.strip()
    if not (is_blank and prev_blank):
        final_lines.append(line)
    prev_blank = is_blank

# Write result
with open("$temp_file", 'w') as f:
    for line in final_lines:
        f.write(line + '\n')
EOF
        
        # Apply if different
        if ! cmp -s "$file" "$temp_file"; then
            mv "$temp_file" "$file"
            files_fixed=$((files_fixed + 1))
            echo "  ✅ FIXED"
        else
            rm "$temp_file"
            echo "  ⏭️  OK"
        fi
    fi
done

echo ""
echo "📊 FIXED $files_fixed FILES"

# Final count
after_count=$(golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
improvement=$((before_count - after_count))

echo ""
echo "🎯 RESULTS:"
echo "   BEFORE: $before_count"
echo "   AFTER:  $after_count" 
echo "   FIXED:  $improvement"

if [ $after_count -eq 0 ]; then
    echo ""
    echo "🏆 PERFECT! ALL WSL_V5 VIOLATIONS ELIMINATED!"
    echo "🚀 FULL AUTOMATION SUCCESS - NO MANUAL WORK NEEDED!"
elif [ $improvement -gt 0 ]; then
    echo ""
    percentage=$(( (improvement * 100) / before_count ))
    echo "📈 $percentage% AUTOMATED SUCCESS!"
    echo "🔄 NEED ONE MORE ITERATION FOR REMAINING $after_count VIOLATIONS"
else
    echo ""
    echo "❌ PATTERNS NEED REFINEMENT FOR REMAINING VIOLATIONS:"
    golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | head -3
fi

echo ""
echo "✨ AUTOMATED WSL_V5 FIX COMPLETE!"