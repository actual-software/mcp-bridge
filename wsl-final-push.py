#!/usr/bin/env python3

import os
import re
import subprocess

def fix_trailing_whitespace_in_file(file_path, line_num):
    """Fix trailing whitespace in specific file and line."""
    try:
        with open(file_path, 'r', encoding='utf-8') as f:
            lines = f.readlines()
        
        if line_num <= len(lines):
            lines[line_num - 1] = lines[line_num - 1].rstrip() + '\n'
            
            with open(file_path, 'w', encoding='utf-8') as f:
                f.writelines(lines)
            return True
    except Exception as e:
        print(f"Error fixing trailing whitespace: {e}")
    return False

def fix_unnecessary_leading_whitespace(file_path, line_num):
    """Remove unnecessary leading whitespace (empty lines)."""
    try:
        with open(file_path, 'r', encoding='utf-8') as f:
            lines = f.readlines()
        
        if line_num <= len(lines) and lines[line_num - 1].strip() == '':
            lines.pop(line_num - 1)
            
            with open(file_path, 'w', encoding='utf-8') as f:
                f.writelines(lines)
            return True
    except Exception as e:
        print(f"Error removing leading whitespace: {e}")
    return False

def add_missing_whitespace(file_path, line_num):
    """Add missing blank line above specified line."""
    try:
        with open(file_path, 'r', encoding='utf-8') as f:
            lines = f.readlines()
        
        if line_num > 1 and line_num <= len(lines):
            # Check if previous line is not blank
            if lines[line_num - 2].strip() != '':
                lines.insert(line_num - 1, '\n')
                
                with open(file_path, 'w', encoding='utf-8') as f:
                    f.writelines(lines)
                return True
    except Exception as e:
        print(f"Error adding whitespace: {e}")
    return False

def main():
    os.chdir('/Users/poile/repos/mcp')
    
    # Get all violations
    result = subprocess.run([
        'golangci-lint', 'run', './services/gateway/...', './services/router/...', 
        '--config', '.golangci.yml'
    ], capture_output=True, text=True)
    
    violations = [line.strip() for line in result.stdout.split('\n') if 'wsl_v5' in line and ':' in line]
    
    print(f"Found {len(violations)} violations to fix")
    
    fixed = 0
    
    for violation in violations:
        parts = violation.split(':')
        if len(parts) < 4:
            continue
            
        file_path = parts[0]
        line_num = int(parts[1])
        message = ':'.join(parts[3:]).strip()
        
        print(f"Fixing: {file_path}:{line_num}")
        
        if 'trailing-whitespace' in message:
            if fix_trailing_whitespace_in_file(file_path, line_num):
                fixed += 1
                print(f"  ✓ Fixed trailing whitespace")
        
        elif 'unnecessary whitespace (leading-whitespace)' in message:
            if fix_unnecessary_leading_whitespace(file_path, line_num):
                fixed += 1
                print(f"  ✓ Removed unnecessary leading whitespace")
        
        elif 'missing whitespace above this line' in message:
            if add_missing_whitespace(file_path, line_num):
                fixed += 1
                print(f"  ✓ Added missing whitespace")
    
    print(f"\nFixed {fixed} violations")
    
    # Check remaining
    result = subprocess.run([
        'golangci-lint', 'run', './services/gateway/...', './services/router/...', 
        '--config', '.golangci.yml'
    ], capture_output=True, text=True)
    
    remaining = [line.strip() for line in result.stdout.split('\n') if 'wsl_v5' in line and ':' in line]
    print(f"Remaining violations: {len(remaining)}")
    
    if len(remaining) == 0:
        print("🎉 SUCCESS! All wsl_v5 violations eliminated!")
    else:
        print("Remaining violations:")
        for v in remaining[:10]:
            print(f"  {v}")

if __name__ == '__main__':
    main()