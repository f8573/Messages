import re
import sys

def process(filepath):
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()

    if 'carrier/handler.go' in filepath.replace('\\', '/'):
        content = content.replace(
            ', fmt.Sprintf("carrier: carrier_message %s is not device-authoritative; reconciliation required", carrierID)\n',
            ', fmt.Sprintf("carrier: carrier_message %s is not device-authoritative; reconciliation required", carrierID), nil)\n'
        )
    elif 'notification/handler.go' in filepath.replace('\\', '/'):
        content = content.replace('} }', '}')
        idx = content.find('type Preferences struct {')
        if idx != -1:
            content = content[:idx]
    
    elif 'auth/handler.go' in filepath.replace('\\', '/'):
        def replacer(m):
            qstring = m.group(2)
            if '`' not in qstring:
                return f"{m.group(1)}`\n{qstring}\n`{m.group(3)}"
            return m.group(0)
        
        content = re.sub(r'(tx\.Exec\(\s*ctx,\s*\n?)(INSERT INTO phone_verification_challenges.*?5 minute\))(\s*\n, id)', replacer, content, flags=re.DOTALL)
        content = re.sub(r'(tx\.QueryRow\(\s*ctx,\s*\n?)(SELECT phone_e164.*?FOR UPDATE)(\s*\n, challengeID)', replacer, content, flags=re.DOTALL)
        content = re.sub(r'(tx\.Exec\(\s*ctx,\s*)(UPDATE phone_verification_challenges SET attempts_remaining = GREATEST\(attempts_remaining - 1, 0\) WHERE id = \$1)(\, challengeID)', replacer, content, flags=re.DOTALL)
        content = re.sub(r'(tx\.Exec\(\s*ctx,\s*)(UPDATE phone_verification_challenges SET consumed_at = now\(\) WHERE id = \$1)(\, challengeID)', replacer, content, flags=re.DOTALL)
        content = re.sub(r'(tx\.QueryRow\(\s*ctx,\s*\n?)(INSERT INTO users \(primary_phone_e164.*?RETURNING id::text)(\s*\n, phoneE164)', replacer, content, flags=re.DOTALL)
        content = re.sub(r'(tx\.Exec\(\s*ctx,\s*\n?)(WITH matched AS \(.*?ON CONFLICT \(conversation_id, user_id\) DO NOTHING)(\s*\n, phoneE164)', replacer, content, flags=re.DOTALL)
        content = re.sub(r'(tx\.Exec\(\s*ctx,\s*\n?)(WITH matched AS \(.*?ON CONFLICT \(conversation_id, external_contact_id\) DO NOTHING)(\s*\n, phoneE164)', replacer, content, flags=re.DOTALL)

    elif 'devicekeys/service.go' in filepath.replace('\\', '/'):
        content = content.replace('\t"context"\n', '')

    elif 'users/service.go' in filepath.replace('\\', '/'):
        content = re.sub(
            r'replication\.UserEventConversationStateUpdated\{\s*ConversationID:\s*conversationID,\s*BlockState:\s*&replication\.BlockState\{\s*ActorBlockedTarget:\s*actorBlocksTarget,\s*TargetBlockedActor:\s*targetBlocksActor,\s*StateUpdatedAtNanos:\s*time\.Now\(\)\.UnixNano\(\),\s*\},\s*\}',
            'conversationID, replication.UserEventConversationStateUpdated, map[string]any{\n\t\t\t\t\t"actor_blocked_target": actorBlocksTarget,\n\t\t\t\t\t"target_blocked_actor": targetBlocksActor,\n\t\t\t\t\t"state_updated_at_nanos": time.Now().UnixNano(),\n\t\t\t\t}',
            content, flags=re.DOTALL)

    with open(filepath, 'w', encoding='utf-8') as f:
        f.write(content)

for p in sys.argv[1:]:
    process(p)
