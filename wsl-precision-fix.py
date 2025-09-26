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
    try:
        line_num = int(parts[1])
    except ValueError:
        return None
    message = ':'.join(parts[3:]).strip()
    
    return {
        'file': file_path,
        'line': line_num,
        'message': message
    }

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
        
        print(f"Processing: {file_path}:{violation['line']} - {message}")
        
        # Handle unnecessary leading whitespace
        if 'unnecessary whitespace (leading-whitespace)' in message:
            if line_idx < len(lines) and lines[line_idx].strip() == '':
                print(f"  Removing empty line at {line_idx + 1}")
                lines.pop(line_idx)
                modified = True
        
        # Handle missing whitespace cases - add blank line before the violation line
        elif 'missing whitespace above this line' in message:
            if line_idx > 0 and line_idx < len(lines):
                # Check if previous line is not already blank
                if lines[line_idx - 1].strip() != '':
                    print(f"  Adding blank line before {line_idx + 1}")
                    lines.insert(line_idx, '')
                    modified = True
                else:
                    print(f"  Blank line already exists before {line_idx + 1}")
            else:
                print(f"  Cannot add blank line (line_idx: {line_idx}, len: {len(lines)})")
    
    if modified:
        try:
            with open(file_path, 'w', encoding='utf-8') as f:
                for line in lines:
                    f.write(line + '\n')
            print(f"  Modified {file_path}")
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
        print(f"\n--- Fixing {file_path} ({len(file_violations)} violations) ---")
        if fix_file_violations(file_path, violations):
            fixed_files += 1
    
    print(f"\nFixed {fixed_files} files")
    
    # Check remaining violations
    print("\nChecking remaining violations...")
    remaining = get_wsl_violations()
    print(f"Remaining violations: {len(remaining)}")
    
    if len(remaining) < len(violations_text):
        print(f"SUCCESS: Reduced violations from {len(violations_text)} to {len(remaining)}")
    
    if remaining:
        print("\nFirst 10 remaining violations:")
        for violation in remaining[:10]:
            print(f"  {violation}")

if __name__ == '__main__':
    main()