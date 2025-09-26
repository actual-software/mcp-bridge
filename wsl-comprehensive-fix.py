#!/usr/bin/env python3

import os
import re
import subprocess
import sys

def get_wsl_violations():
    """Get all wsl_v5 violations from golangci-lint."""
    try:
        result = subprocess.run([
            'golangci-lint', 'run', './services/gateway/...', './services/router/...', 
            '--config', '.golangci.yml'
        ], capture_output=True, text=True, cwd='/Users/poile/repos/mcp')
        
        violations = []
        for line in result.stdout.split('\n'):
            if 'wsl_v5' in line and ':' in line:
                violations.append(line.strip())
        
        return violations
    except Exception as e:
        print(f"Error getting violations: {e}")
        return []

def parse_violation(violation_line):
    """Parse a violation line to extract file, line number, and violation type."""
    if not violation_line:
        return None
        
    # Format: path:line:col: message (wsl_v5)
    parts = violation_line.split(':')
    if len(parts) < 4:
        return None
        
    file_path = parts[0]
    line_num = int(parts[1])
    message = ':'.join(parts[3:]).strip()
    
    return {
        'file': file_path,
        'line': line_num,
        'message': message
    }

def fix_leading_whitespace(lines, line_idx):
    """Fix unnecessary leading whitespace violations."""
    if line_idx >= len(lines):
        return False
        
    line = lines[line_idx]
    # Remove empty line if it's truly unnecessary
    if line.strip() == '' and line_idx > 0:
        # Check if previous line ends with {, }, or similar
        prev_line = lines[line_idx - 1].strip()
        if prev_line.endswith(('{', '}', ';', ')', ',')) or prev_line.startswith(('func ', 'type ', 'var ', 'const ')):
            lines.pop(line_idx)
            return True
    
    return False

def fix_missing_whitespace_above(lines, line_idx, violation_type):
    """Fix missing whitespace above this line violations."""
    if line_idx <= 0 or line_idx >= len(lines):
        return False
    
    current_line = lines[line_idx].strip()
    prev_line = lines[line_idx - 1].strip()
    
    # Don't add blank line if one already exists
    if prev_line == '':
        return False
    
    # Specific patterns that need blank lines above
    needs_blank_line = False
    
    if 'defer' in violation_type:
        # defer statements need blank line above in certain contexts
        if current_line.startswith('defer '):
            needs_blank_line = True
    
    elif 'go' in violation_type:
        # go statements need blank line above  
        if current_line.startswith('go '):
            needs_blank_line = True
    
    elif 'if' in violation_type:
        # if statements need blank line above in certain contexts
        if current_line.startswith('if '):
            # Check if previous line is not a close brace or another control structure
            if not prev_line.endswith(('{', '}')) and not prev_line.startswith(('if ', 'for ', 'switch ', 'case ', 'default:')):
                needs_blank_line = True
    
    elif 'for' in violation_type:
        # for loops need blank line above
        if current_line.startswith('for '):
            needs_blank_line = True
    
    elif 'decl' in violation_type:
        # declarations need blank line above
        if current_line.startswith(('var ', 'const ', 'type ', 'func ')):
            needs_blank_line = True
    
    elif 'return' in violation_type:
        # return statements sometimes need blank line above
        if current_line.startswith('return'):
            needs_blank_line = True
    
    if needs_blank_line:
        lines.insert(line_idx, '')
        return True
    
    return False

def fix_file_violations(file_path, violations):
    """Fix all violations in a single file."""
    try:
        with open(file_path, 'r', encoding='utf-8') as f:
            lines = f.readlines()
    except Exception as e:
        print(f"Error reading {file_path}: {e}")
        return False
    
    # Remove trailing newlines but preserve line structure
    lines = [line.rstrip('\n\r') for line in lines]
    
    # Sort violations by line number in reverse order to avoid line number shifts
    file_violations = [v for v in violations if v['file'] == file_path]
    file_violations.sort(key=lambda x: x['line'], reverse=True)
    
    modified = False
    
    for violation in file_violations:
        line_idx = violation['line'] - 1  # Convert to 0-based indexing
        message = violation['message']
        
        if 'unnecessary whitespace (leading-whitespace)' in message:
            if fix_leading_whitespace(lines, line_idx):
                modified = True
                print(f"Fixed leading whitespace at {file_path}:{violation['line']}")
        
        elif 'missing whitespace above this line' in message:
            if fix_missing_whitespace_above(lines, line_idx, message):
                modified = True
                print(f"Added blank line above {file_path}:{violation['line']}")
    
    if modified:
        try:
            with open(file_path, 'w', encoding='utf-8') as f:
                for line in lines:
                    f.write(line + '\n')
            return True
        except Exception as e:
            print(f"Error writing {file_path}: {e}")
    
    return False

def main():
    os.chdir('/Users/poile/repos/mcp')
    
    print("Getting current wsl_v5 violations...")
    violations_text = get_wsl_violations()
    
    if not violations_text:
        print("No wsl_v5 violations found!")
        return
    
    print(f"Found {len(violations_text)} violations")
    
    # Parse violations
    violations = []
    for v_text in violations_text:
        parsed = parse_violation(v_text)
        if parsed:
            violations.append(parsed)
    
    if not violations:
        print("No valid violations parsed!")
        return
    
    print(f"Parsed {len(violations)} valid violations")
    
    # Group by file
    files_to_fix = {}
    for violation in violations:
        file_path = violation['file']
        if file_path not in files_to_fix:
            files_to_fix[file_path] = []
        files_to_fix[file_path].append(violation)
    
    print(f"Files to fix: {len(files_to_fix)}")
    
    # Fix each file
    fixed_files = 0
    for file_path, file_violations in files_to_fix.items():
        print(f"\nFixing {file_path} ({len(file_violations)} violations)...")
        if fix_file_violations(file_path, violations):
            fixed_files += 1
    
    print(f"\nFixed {fixed_files} files")
    
    # Check remaining violations
    print("\nChecking remaining violations...")
    remaining = get_wsl_violations()
    print(f"Remaining violations: {len(remaining)}")
    
    if remaining:
        print("Sample remaining violations:")
        for violation in remaining[:5]:
            print(f"  {violation}")

if __name__ == '__main__':
    main()