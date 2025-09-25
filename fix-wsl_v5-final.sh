#!/bin/bash

# Final working wsl_v5 fixer - based on successful manual fixes

set -e

echo "🚀 Final wsl_v5 fixer - using proven working patterns..."

cd /Users/poile/repos/mcp

# Count issues before
before_count=$(golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
echo "📊 Found $before_count wsl_v5 issues before fixing"

if [ "$before_count" -eq 0 ]; then
    echo "🎉 No wsl_v5 issues found!"
    exit 0
fi

# Get specific files and their exact line issues
echo "🔍 Analyzing specific violations..."
violations_file="/tmp/wsl_v5_violations.txt"
golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" > "$violations_file"

# Group violations by type
missing_whitespace_files=$(grep "missing whitespace" "$violations_file" | cut -d: -f1 | sort -u)
unnecessary_whitespace_files=$(grep "unnecessary whitespace" "$violations_file" | cut -d: -f1 | sort -u)

echo "Files with missing whitespace issues:"
echo "$missing_whitespace_files" | head -3
echo "Files with unnecessary whitespace issues:"  
echo "$unnecessary_whitespace_files" | head -3

# Function to add missing blank lines
fix_missing_whitespace() {
    local file="$1"
    local temp_file="${file}.tmp"
    
    awk '
    BEGIN { prev_line = "" }
    {
        current_line = $0
        
        # Add blank line before nested if statements that need spacing
        # This matches the pattern: require.Error(t, err) followed by if tt.errMsg != ""
        if (current_line ~ /^\s+if .* != ""/ && prev_line ~ /require\.Error/) {
            print ""
        }
        
        print current_line
        prev_line = current_line
    }
    ' "$file" > "$temp_file"
    
    if ! cmp -s "$file" "$temp_file"; then
        mv "$temp_file" "$file"
        return 0
    else
        rm "$temp_file" 
        return 1
    fi
}

# Function to remove unnecessary blank lines  
fix_unnecessary_whitespace() {
    local file="$1"
    local temp_file="${file}.tmp"
    
    awk '
    BEGIN { prev_line = ""; prev_blank = 0 }
    {
        current_line = $0
        current_blank = (current_line ~ /^\s*$/)
        
        # Remove unnecessary blank lines in certain contexts
        if (current_blank && prev_blank && prev_line ~ /^\s*$/) {
            next  # Skip this blank line
        }
        
        print current_line
        prev_line = current_line  
        prev_blank = current_blank
    }
    ' "$file" > "$temp_file"
    
    if ! cmp -s "$file" "$temp_file"; then
        mv "$temp_file" "$file"
        return 0
    else
        rm "$temp_file"
        return 1
    fi
}

files_fixed=0

# Fix missing whitespace issues
echo "🔧 Fixing missing whitespace issues..."
for file in $missing_whitespace_files; do
    if [ -f "$file" ]; then
        echo "  Processing: $file"
        if fix_missing_whitespace "$file"; then
            files_fixed=$((files_fixed + 1))
            echo "    ✅ Added blank lines to $file"
        fi
    fi
done

# Fix unnecessary whitespace issues  
echo "🧹 Fixing unnecessary whitespace issues..."
for file in $unnecessary_whitespace_files; do
    if [ -f "$file" ]; then
        echo "  Processing: $file"
        if fix_unnecessary_whitespace "$file"; then
            files_fixed=$((files_fixed + 1))
            echo "    ✅ Removed extra blank lines from $file"
        fi
    fi
done

echo "📝 Modified $files_fixed files total"

# Final count
echo ""
echo "📊 Counting wsl_v5 issues after fixing..."
after_count=$(golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
echo "Found $after_count wsl_v5 issues after fixing"

improvement=$((before_count - after_count))
if [ $improvement -gt 0 ]; then
    echo "🎉 SUCCESS: Fixed $improvement wsl_v5 issues! ($before_count → $after_count)"
    percentage=$(( (improvement * 100) / before_count ))
    echo "📈 Improvement: $percentage% of issues resolved"
    
    if [ $after_count -gt 0 ]; then
        echo "📋 Remaining issues to investigate:"
        golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | head -3
    fi
else
    echo "⚠️  No improvement detected."
    echo "📋 Sample remaining issues:"
    head -3 "$violations_file"
fi

# Cleanup
rm -f "$violations_file"

echo "✨ Final wsl_v5 fix completed!"