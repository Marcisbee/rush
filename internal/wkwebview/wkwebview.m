#import "wkwebview.h"

#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <WebKit/WebKit.h>

struct rush_wk_view {
  void *window;
  void *webview;
  void *handler;
  uintptr_t handle;
  int debug;
};

@interface RushWKMessageHandler : NSObject <WKScriptMessageHandler>
@property(nonatomic, assign) uintptr_t handle;
@end

@implementation RushWKMessageHandler
- (void)userContentController:(WKUserContentController *)controller
      didReceiveScriptMessage:(WKScriptMessage *)message {
  (void)controller;
  NSString *payload = nil;
  if ([message.body isKindOfClass:[NSString class]]) {
    payload = (NSString *)message.body;
  } else if ([NSJSONSerialization isValidJSONObject:message.body]) {
    NSData *data = [NSJSONSerialization dataWithJSONObject:message.body options:0 error:nil];
    payload = [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];
  }
  if (payload != nil) {
    goRushWKMessage(self.handle, (char *)payload.UTF8String);
  }
}
@end

static WKWebView *rush_webview(rush_wk_view *view) {
  return view == NULL ? nil : (__bridge WKWebView *)view->webview;
}

static NSWindow *rush_window(rush_wk_view *view) {
  return view == NULL ? nil : (__bridge NSWindow *)view->window;
}

static NSString *rush_string(const char *value) {
  return value == NULL ? @"" : [NSString stringWithUTF8String:value];
}

static void rush_on_main(void (^work)(void)) {
  if ([NSThread isMainThread]) {
    work();
  } else {
    dispatch_async(dispatch_get_main_queue(), work);
  }
}

rush_wk_view *rush_wk_create(int debug, uintptr_t handle) {
  if (![NSThread isMainThread]) {
    return NULL;
  }
  [NSApplication sharedApplication];
  if (debug) {
    [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
  } else {
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
  }

  RushWKMessageHandler *handler = [RushWKMessageHandler new];
  handler.handle = handle;
  WKUserContentController *content = [WKUserContentController new];
  [content addScriptMessageHandler:handler name:@"rush"];
  WKWebViewConfiguration *configuration = [WKWebViewConfiguration new];
  configuration.userContentController = content;
  configuration.websiteDataStore = [WKWebsiteDataStore nonPersistentDataStore];

  NSRect frame = NSMakeRect(0, 0, 1280, 800);
  WKWebView *webview = [[WKWebView alloc] initWithFrame:frame configuration:configuration];
  if (@available(macOS 13.3, *)) {
    webview.inspectable = YES;
  }
  NSWindow *window = [[NSWindow alloc]
      initWithContentRect:frame
                styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
                           NSWindowStyleMaskResizable | NSWindowStyleMaskMiniaturizable)
                  backing:NSBackingStoreBuffered
                    defer:NO];
  window.contentView = webview;
  if (debug) {
    [window makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
  } else {
    [window orderOut:nil];
  }

  rush_wk_view *view = calloc(1, sizeof(rush_wk_view));
  if (view == NULL) {
    [window close];
    return NULL;
  }
  view->window = (__bridge_retained void *)window;
  view->webview = (__bridge_retained void *)webview;
  view->handler = (__bridge_retained void *)handler;
  view->handle = handle;
  view->debug = debug;
  return view;
}

void rush_wk_run(rush_wk_view *view) {
  (void)view;
  [NSApp run];
}

void rush_wk_terminate(rush_wk_view *view) {
  (void)view;
  rush_on_main(^{
    [NSApp stop:nil];
    NSEvent *wake = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
                                        location:NSZeroPoint
                                   modifierFlags:0
                                       timestamp:0
                                    windowNumber:0
                                         context:nil
                                         subtype:0
                                           data1:0
                                           data2:0];
    [NSApp postEvent:wake atStart:NO];
  });
}

void rush_wk_destroy(rush_wk_view *view) {
  if (view == NULL) return;
  void (^destroy)(void) = ^{
    WKWebView *webview = rush_webview(view);
    NSWindow *window = rush_window(view);
    [webview stopLoading];
    [webview.configuration.userContentController removeScriptMessageHandlerForName:@"rush"];
    window.contentView = nil;
    [window close];
    __unused id handler = CFBridgingRelease(view->handler);
    __unused id releasedWebView = CFBridgingRelease(view->webview);
    __unused id releasedWindow = CFBridgingRelease(view->window);
    free(view);
  };
  if ([NSThread isMainThread]) destroy();
  else dispatch_sync(dispatch_get_main_queue(), destroy);
}

void rush_wk_dispatch(rush_wk_view *view, uintptr_t token) {
  (void)view;
  rush_on_main(^{ goRushWKDispatch(token); });
}

void *rush_wk_window(rush_wk_view *view) {
  return view == NULL ? NULL : view;
}

void rush_wk_set_title(rush_wk_view *view, const char *title) {
  NSString *value = rush_string(title);
  rush_on_main(^{ rush_window(view).title = value; });
}

void rush_wk_set_size(rush_wk_view *view, int width, int height, int hint) {
  (void)hint;
  rush_on_main(^{
    NSWindow *window = rush_window(view);
    [window setContentSize:NSMakeSize(width, height)];
    rush_webview(view).frame = NSMakeRect(0, 0, width, height);
  });
}

void rush_wk_navigate(rush_wk_view *view, const char *url) {
  NSString *value = rush_string(url);
  rush_on_main(^{
    NSURL *target = [NSURL URLWithString:value];
    if (target != nil) [rush_webview(view) loadRequest:[NSURLRequest requestWithURL:target]];
  });
}

void rush_wk_set_html(rush_wk_view *view, const char *html) {
  NSString *value = rush_string(html);
  rush_on_main(^{ [rush_webview(view) loadHTMLString:value baseURL:nil]; });
}

void rush_wk_init(rush_wk_view *view, const char *javascript) {
  NSString *source = rush_string(javascript);
  rush_on_main(^{
    WKUserScript *script = [[WKUserScript alloc] initWithSource:source
                                                  injectionTime:WKUserScriptInjectionTimeAtDocumentStart
                                               forMainFrameOnly:NO];
    [rush_webview(view).configuration.userContentController addUserScript:script];
    [rush_webview(view) evaluateJavaScript:source completionHandler:nil];
  });
}

void rush_wk_eval(rush_wk_view *view, const char *javascript) {
  NSString *source = rush_string(javascript);
  rush_on_main(^{ [rush_webview(view) evaluateJavaScript:source completionHandler:nil]; });
}

void rush_wk_evaluate(rush_wk_view *view, const char *javascript, uintptr_t token) {
  NSString *source = rush_string(javascript);
  rush_on_main(^{
    [rush_webview(view) evaluateJavaScript:source completionHandler:^(id value, NSError *error) {
      if (error != nil) {
        goRushWKEvaluation(token, NULL, (char *)error.localizedDescription.UTF8String);
        return;
      }
      if (value == nil) {
        goRushWKEvaluation(token, (char *)"null", NULL);
        return;
      }
      id object = value;
      if (![NSJSONSerialization isValidJSONObject:object]) object = @[value];
      NSError *serializationError = nil;
      NSData *data = [NSJSONSerialization dataWithJSONObject:object options:0 error:&serializationError];
      if (data == nil || serializationError != nil) {
        goRushWKEvaluation(token, NULL, (char *)serializationError.localizedDescription.UTF8String);
        return;
      }
      NSString *json = [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];
      if (![NSJSONSerialization isValidJSONObject:value]) {
        json = [json substringWithRange:NSMakeRange(1, json.length - 2)];
      }
      goRushWKEvaluation(token, (char *)json.UTF8String, NULL);
    }];
  });
}

void rush_wk_snapshot(rush_wk_view *view, uintptr_t token) {
  rush_on_main(^{
    [rush_webview(view) takeSnapshotWithConfiguration:nil completionHandler:^(NSImage *image, NSError *error) {
      if (image == nil || error != nil) {
        const char *message = error == nil ? "WKWebView did not return a snapshot" : error.localizedDescription.UTF8String;
        goRushWKSnapshot(token, NULL, 0, (char *)message);
        return;
      }
      NSBitmapImageRep *bitmap = [[NSBitmapImageRep alloc] initWithData:image.TIFFRepresentation];
      NSData *png = [bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
      if (png == nil) {
        goRushWKSnapshot(token, NULL, 0, (char *)"WKWebView snapshot could not be encoded as PNG");
        return;
      }
      goRushWKSnapshot(token, (void *)png.bytes, (int)png.length, NULL);
    }];
  });
}

int rush_wk_content_origin(rush_wk_view *view, double *x, double *y) {
  NSWindow *window = rush_window(view);
  WKWebView *webview = rush_webview(view);
  if (window == nil || webview == nil || !window.visible || !view->debug) return 1;
  [window makeKeyAndOrderFront:nil];
  NSPoint point = [webview convertPoint:NSMakePoint(0, 0) toView:nil];
  point = [window convertPointToScreen:point];
  NSScreen *screen = NSScreen.screens.firstObject;
  *x = point.x;
  *y = screen.frame.size.height - (point.y + webview.frame.size.height);
  return 0;
}

static int rush_accessibility(void) { return AXIsProcessTrusted() ? 0 : 2; }

int rush_wk_trusted_click(double x, double y) {
  int permission = rush_accessibility();
  if (permission != 0) return permission;
  CGPoint point = CGPointMake(x, y);
  CGEventRef down = CGEventCreateMouseEvent(NULL, kCGEventLeftMouseDown, point, kCGMouseButtonLeft);
  CGEventRef up = CGEventCreateMouseEvent(NULL, kCGEventLeftMouseUp, point, kCGMouseButtonLeft);
  if (down == NULL || up == NULL) {
    if (down) CFRelease(down);
    if (up) CFRelease(up);
    return 3;
  }
  CGEventPost(kCGHIDEventTap, down);
  CGEventPost(kCGHIDEventTap, up);
  CFRelease(down);
  CFRelease(up);
  return 0;
}

static int rush_post_text(NSString *text) {
  int permission = rush_accessibility();
  if (permission != 0) return permission;
  CGEventRef down = CGEventCreateKeyboardEvent(NULL, 0, true);
  CGEventRef up = CGEventCreateKeyboardEvent(NULL, 0, false);
  if (down == NULL || up == NULL) {
    if (down) CFRelease(down);
    if (up) CFRelease(up);
    return 3;
  }
  NSUInteger length = text.length;
  UniChar *characters = calloc(length, sizeof(UniChar));
  [text getCharacters:characters range:NSMakeRange(0, length)];
  CGEventKeyboardSetUnicodeString(down, length, characters);
  CGEventKeyboardSetUnicodeString(up, length, characters);
  CGEventPost(kCGHIDEventTap, down);
  CGEventPost(kCGHIDEventTap, up);
  free(characters);
  CFRelease(down);
  CFRelease(up);
  return 0;
}

int rush_wk_trusted_type(const char *text) { return rush_post_text(rush_string(text)); }

int rush_wk_trusted_press(const char *key) {
  NSDictionary<NSString *, NSNumber *> *codes = @{
    @"Enter": @36, @"Tab": @48, @"Escape": @53, @"Backspace": @51,
    @"ArrowLeft": @123, @"ArrowRight": @124, @"ArrowDown": @125, @"ArrowUp": @126,
    @"Space": @49,
    @"a": @0, @"s": @1, @"d": @2, @"f": @3, @"h": @4, @"g": @5,
    @"z": @6, @"x": @7, @"c": @8, @"v": @9, @"b": @11, @"q": @12,
    @"w": @13, @"e": @14, @"r": @15, @"y": @16, @"t": @17, @"o": @31,
    @"u": @32, @"i": @34, @"p": @35, @"l": @37, @"j": @38, @"k": @40,
    @"n": @45, @"m": @46,
    @"1": @18, @"2": @19, @"3": @20, @"4": @21, @"6": @22, @"5": @23,
    @"9": @25, @"7": @26, @"8": @28, @"0": @29,
  };
  NSArray<NSString *> *parts = [rush_string(key) componentsSeparatedByString:@"+"];
  NSString *keyName = parts.lastObject;
  NSNumber *code = codes[keyName];
  if (code == nil) code = codes[keyName.lowercaseString];
  if (code == nil) return 4;
  int permission = rush_accessibility();
  if (permission != 0) return permission;
  CGEventRef down = CGEventCreateKeyboardEvent(NULL, code.unsignedShortValue, true);
  CGEventRef up = CGEventCreateKeyboardEvent(NULL, code.unsignedShortValue, false);
  if (down == NULL || up == NULL) {
    if (down) CFRelease(down);
    if (up) CFRelease(up);
    return 3;
  }
  CGEventFlags flags = 0;
  for (NSString *modifier in [parts subarrayWithRange:NSMakeRange(0, parts.count - 1)]) {
    if ([modifier caseInsensitiveCompare:@"Shift"] == NSOrderedSame) flags |= kCGEventFlagMaskShift;
    else if ([modifier caseInsensitiveCompare:@"Control"] == NSOrderedSame) flags |= kCGEventFlagMaskControl;
    else if ([modifier caseInsensitiveCompare:@"Alt"] == NSOrderedSame || [modifier caseInsensitiveCompare:@"Option"] == NSOrderedSame) flags |= kCGEventFlagMaskAlternate;
    else if ([modifier caseInsensitiveCompare:@"Meta"] == NSOrderedSame || [modifier caseInsensitiveCompare:@"Command"] == NSOrderedSame) flags |= kCGEventFlagMaskCommand;
    else return 4;
  }
  CGEventSetFlags(down, flags);
  CGEventSetFlags(up, flags);
  CGEventPost(kCGHIDEventTap, down);
  CGEventPost(kCGHIDEventTap, up);
  CFRelease(down);
  CFRelease(up);
  return 0;
}
