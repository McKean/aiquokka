#include "keychain_darwin.h"

static const char *claudeCredentialsService = "Claude Code-credentials";

OSStatus findClaudeCredentials(CFDataRef *data, CFDataRef *persistentRef) {
  CFStringRef service = CFStringCreateWithCString(NULL, claudeCredentialsService, kCFStringEncodingUTF8);
  CFMutableDictionaryRef query = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
  CFDictionarySetValue(query, kSecClass, kSecClassGenericPassword);
  CFDictionarySetValue(query, kSecAttrService, service);
  CFDictionarySetValue(query, kSecReturnPersistentRef, kCFBooleanTrue);
  CFDictionarySetValue(query, kSecMatchLimit, kSecMatchLimitAll);

  CFTypeRef result = NULL;
  OSStatus status = SecItemCopyMatching(query, &result);
  CFRelease(query);
  CFRelease(service);
  if (status != errSecSuccess) return status;

  CFArrayRef matches = (CFArrayRef)result;
  if (CFArrayGetCount(matches) != 1) { CFRelease(result); return errSecDuplicateItem; }
  *persistentRef = (CFDataRef)CFArrayGetValueAtIndex(matches, 0);
  if (*persistentRef == NULL) { CFRelease(result); return errSecDecode; }
  CFRetain(*persistentRef);
  CFRelease(result);

  status = readClaudeCredentials(*persistentRef, data);
  if (status != errSecSuccess) CFRelease(*persistentRef);
  return status;
}

OSStatus readClaudeCredentials(CFDataRef persistentRef, CFDataRef *data) {
  CFMutableDictionaryRef query = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
  CFDictionarySetValue(query, kSecValuePersistentRef, persistentRef);
  CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
  CFTypeRef credential = NULL;
  OSStatus status = SecItemCopyMatching(query, &credential);
  CFRelease(query);
  if (status != errSecSuccess) return status;
  *data = (CFDataRef)credential;
  return errSecSuccess;
}

OSStatus updateClaudeCredentials(CFDataRef persistentRef, CFDataRef data) {
  CFMutableDictionaryRef query = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
  CFMutableDictionaryRef values = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
  CFDictionarySetValue(query, kSecValuePersistentRef, persistentRef);
  CFDictionarySetValue(values, kSecValueData, data);
  OSStatus status = SecItemUpdate(query, values);
  CFRelease(values);
  CFRelease(query);
  return status;
}

CFDataRef dataFromBytes(const void *bytes, CFIndex length) {
  return CFDataCreate(NULL, bytes, length);
}
