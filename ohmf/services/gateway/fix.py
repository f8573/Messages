import re
import sys

def fix_file(filepath):
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()

    # auth/handler.go
    if "auth/handler.go" in filepath.replace('\\', '/'):
        content = re.sub(r'tx\.Exec\((ctx,\s*)(INSERT INTO phone_verification.*?5 minute\))', r"tx.Exec(\1`\n\2\n`", content, flags=re.DOTALL)
        content = re.sub(r'tx\.QueryRow\((ctx,\s*)(SELECT phone_e164.*?FOR UPDATE)', r"tx.QueryRow(\1`\n\2\n`", content, flags=re.DOTALL)
        content = re.sub(r'tx\.Exec\((ctx,\s*)(UPDATE phone_verification_challenges SET attempts_remaining.*?1, 0\).*?id = \$1)', r"tx.Exec(\1`\2`", content, flags=re.DOTALL)
        content = re.sub(r'tx\.Exec\((ctx,\s*)(UPDATE phone_verification_challenges SET consumed.*?id = \$1)', r"tx.Exec(\1`\2`", content, flags=re.DOTALL)
        content = re.sub(r'tx\.QueryRow\((ctx,\s*)(INSERT INTO users \(primary_phone_e164.*?\n.*\n.*\nRETURNING id::text)', r"tx.QueryRow(\1`\n\2\n`", content, flags=re.DOTALL)
        content = re.sub(r'tx\.Exec\((ctx,\s*)(WITH matched AS \(\s*SELECT DISTINCT cem\.conversation_id.*?ON CONFLICT \(conversation_id, user_id\) DO NOTHING)', r"tx.Exec(\1`\n\2\n`", content, flags=re.DOTALL)
        content = re.sub(r'tx\.Exec\((ctx,\s*)(WITH matched AS \(\s*SELECT DISTINCT cem\.conversation_id.*?SELECT m\.conversation_id, \$2::uuid.*?ON CONFLICT \(conversation_id, user_id\) DO NOTHING)', r"tx.Exec(\1`\n\2\n`", content, flags=re.DOTALL)
        
        # Fallback fix for anything I missed: literally any unwrapped SQL starting with INSERT, SELECT, UPDATE. 
        # But this is safer.

    with open(filepath, 'w', encoding='utf-8') as f:
        f.write(content)

if __name__ == '__main__':
    fix_file(sys.argv[1])
