#ifndef AIQUOKKA_INTERNAL_CLAUDE_KEYCHAIN_DARWIN_H
#define AIQUOKKA_INTERNAL_CLAUDE_KEYCHAIN_DARWIN_H

#include <Security/Security.h>

OSStatus findClaudeCredentials(CFDataRef *data, CFDataRef *persistentRef);
OSStatus readClaudeCredentials(CFDataRef persistentRef, CFDataRef *data);
OSStatus updateClaudeCredentials(CFDataRef persistentRef, CFDataRef data);
CFDataRef dataFromBytes(const void *bytes, CFIndex length);

#endif
