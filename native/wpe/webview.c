#include <glib.h>
#include <jsc/jsc.h>
#include <wpe/headless/wpe-headless.h>
#include <wpe/webkit.h>

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct webview_instance* webview_t;
typedef void (*webview_dispatch_fn)(webview_t, void*);
typedef void (*webview_bind_fn)(const char*, const char*, void*);

struct binding {
    webview_bind_fn fn;
    void* arg;
};

struct webview_instance {
    GMainLoop* loop;
    WebKitWebView* view;
    WebKitUserContentManager* manager;
    GHashTable* bindings;
};

static gchar* js_string(const char* input)
{
    GString* output = g_string_new("\"");
    const unsigned char* cursor = (const unsigned char*)(input ? input : "");
    for (; *cursor; cursor++) {
        switch (*cursor) {
        case '\\': g_string_append(output, "\\\\"); break;
        case '"': g_string_append(output, "\\\""); break;
        case '\b': g_string_append(output, "\\b"); break;
        case '\f': g_string_append(output, "\\f"); break;
        case '\n': g_string_append(output, "\\n"); break;
        case '\r': g_string_append(output, "\\r"); break;
        case '\t': g_string_append(output, "\\t"); break;
        default:
            if (*cursor < 0x20)
                g_string_append_printf(output, "\\u%04x", *cursor);
            else
                g_string_append_c(output, *cursor);
        }
    }
    g_string_append_c(output, '"');
    return g_string_free(output, FALSE);
}

static void eval(webview_t webview, const char* script)
{
    webkit_web_view_evaluate_javascript(webview->view, script, -1, NULL, NULL,
        NULL, NULL, NULL);
}

static void on_message(WebKitUserContentManager* manager, JSCValue* value,
    webview_t webview)
{
    (void)manager;
    JSCValue* name_value = jsc_value_object_get_property(value, "name");
    JSCValue* id_value = jsc_value_object_get_property(value, "id");
    JSCValue* args_value = jsc_value_object_get_property(value, "args");
    if (!name_value || !id_value || !args_value)
        goto done;

    gchar* name = jsc_value_to_string(name_value);
    gchar* id = jsc_value_to_string(id_value);
    gchar* args = jsc_value_to_string(args_value);
    struct binding* binding = g_hash_table_lookup(webview->bindings, name);
    if (binding)
        binding->fn(id, args, binding->arg);
    g_free(name);
    g_free(id);
    g_free(args);

done:
    g_clear_object(&name_value);
    g_clear_object(&id_value);
    g_clear_object(&args_value);
}

static void add_script(webview_t webview, const char* source)
{
    WebKitUserScript* script = webkit_user_script_new(source,
        WEBKIT_USER_CONTENT_INJECT_ALL_FRAMES,
        WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START, NULL, NULL);
    webkit_user_content_manager_add_script(webview->manager, script);
    webkit_user_script_unref(script);
}

webview_t webview_create(int debug, void* window)
{
    (void)window;
    webview_t webview = g_new0(struct webview_instance, 1);
    webview->loop = g_main_loop_new(NULL, FALSE);
    webview->manager = webkit_user_content_manager_new();
    webview->bindings = g_hash_table_new_full(g_str_hash, g_str_equal, g_free, g_free);
    g_signal_connect(webview->manager,
        "script-message-received::__wpe_webview", G_CALLBACK(on_message), webview);
    if (!webkit_user_content_manager_register_script_message_handler(
            webview->manager, "__wpe_webview", NULL))
        goto fail;

    WPEDisplay* display = wpe_display_headless_new();
    WebKitSettings* settings = webkit_settings_new_with_settings(
        "enable-developer-extras", debug != 0, NULL);
    webview->view = WEBKIT_WEB_VIEW(g_object_new(WEBKIT_TYPE_WEB_VIEW,
        "display", display,
        "settings", settings,
        "user-content-manager", webview->manager,
        NULL));
    g_object_unref(settings);
    g_object_unref(display);
    if (!webview->view)
        goto fail;

    add_script(webview,
        "(()=>{if(window.__wpeWebview)return;let next=0;const pending=new Map();"
        "window.__wpeWebview={call:(name,args)=>new Promise((resolve,reject)=>{"
        "const id=String(++next);pending.set(id,{resolve,reject});"
        "window.webkit.messageHandlers.__wpe_webview.postMessage({name,id,args:JSON.stringify(args)});}),"
        "reply:(id,status,result)=>{const callback=pending.get(String(id));if(!callback)return;"
        "pending.delete(String(id));let value;try{value=JSON.parse(result)}catch(error){callback.reject(error);return}"
        "(status===0?callback.resolve:callback.reject)(value)}}})()");
    return webview;

fail:
    if (webview->view)
        g_object_unref(webview->view);
    if (webview->manager)
        g_object_unref(webview->manager);
    if (webview->bindings)
        g_hash_table_unref(webview->bindings);
    if (webview->loop)
        g_main_loop_unref(webview->loop);
    g_free(webview);
    return NULL;
}

void webview_destroy(webview_t webview)
{
    if (!webview)
        return;
    g_signal_handlers_disconnect_by_data(webview->manager, webview);
    g_clear_object(&webview->view);
    g_clear_object(&webview->manager);
    g_clear_pointer(&webview->bindings, g_hash_table_unref);
    g_clear_pointer(&webview->loop, g_main_loop_unref);
    g_free(webview);
}

void webview_run(webview_t webview)
{
    g_main_loop_run(webview->loop);
}

static gboolean quit_loop(gpointer data)
{
    g_main_loop_quit(((webview_t)data)->loop);
    return G_SOURCE_REMOVE;
}

void webview_terminate(webview_t webview)
{
    g_main_context_invoke(NULL, quit_loop, webview);
}

struct dispatch_request {
    webview_t webview;
    webview_dispatch_fn fn;
    void* arg;
};

static gboolean dispatch_on_main(gpointer data)
{
    struct dispatch_request* request = data;
    request->fn(request->webview, request->arg);
    g_free(request);
    return G_SOURCE_REMOVE;
}

void webview_dispatch(webview_t webview, webview_dispatch_fn fn, void* arg)
{
    struct dispatch_request* request = g_new(struct dispatch_request, 1);
    *request = (struct dispatch_request){webview, fn, arg};
    g_main_context_invoke(NULL, dispatch_on_main, request);
}

void* webview_get_window(webview_t webview)
{
    return webkit_web_view_get_wpe_view(webview->view);
}

void webview_set_title(webview_t webview, const char* title)
{
    WPEView* view = webkit_web_view_get_wpe_view(webview->view);
    if (view)
        wpe_toplevel_set_title(wpe_view_get_toplevel(view), title);
}

void webview_set_size(webview_t webview, int width, int height, int hints)
{
    (void)hints;
    WPEView* view = webkit_web_view_get_wpe_view(webview->view);
    if (view)
        wpe_toplevel_resize(wpe_view_get_toplevel(view), width, height);
}

void webview_navigate(webview_t webview, const char* url)
{
    webkit_web_view_load_uri(webview->view, url);
}

void webview_set_html(webview_t webview, const char* html)
{
    webkit_web_view_load_html(webview->view, html, NULL);
}

void webview_init(webview_t webview, const char* js)
{
    add_script(webview, js);
}

void webview_eval(webview_t webview, const char* js)
{
    eval(webview, js);
}

void webview_bind(webview_t webview, const char* name, webview_bind_fn fn, void* arg)
{
    struct binding* binding = g_new(struct binding, 1);
    *binding = (struct binding){fn, arg};
    g_hash_table_replace(webview->bindings, g_strdup(name), binding);
    gchar* encoded = js_string(name);
    gchar* script = g_strdup_printf(
        "window[%s]=(...args)=>window.__wpeWebview.call(%s,args)", encoded, encoded);
    add_script(webview, script);
    eval(webview, script);
    g_free(script);
    g_free(encoded);
}

void webview_unbind(webview_t webview, const char* name)
{
    g_hash_table_remove(webview->bindings, name);
    gchar* encoded = js_string(name);
    gchar* script = g_strdup_printf("delete window[%s]", encoded);
    eval(webview, script);
    g_free(script);
    g_free(encoded);
}

struct return_request {
    webview_t webview;
    gchar* id;
    int status;
    gchar* result;
};

static gboolean return_on_main(gpointer data)
{
    struct return_request* request = data;
    gchar* id = js_string(request->id);
    gchar* result = js_string(request->result);
    gchar* script = g_strdup_printf("window.__wpeWebview.reply(%s,%d,%s)",
        id, request->status, result);
    eval(request->webview, script);
    g_free(script);
    g_free(result);
    g_free(id);
    g_free(request->result);
    g_free(request->id);
    g_free(request);
    return G_SOURCE_REMOVE;
}

void webview_return(webview_t webview, const char* id, int status, const char* result)
{
    struct return_request* request = g_new(struct return_request, 1);
    *request = (struct return_request){webview, g_strdup(id), status, g_strdup(result)};
    g_main_context_invoke(NULL, return_on_main, request);
}
