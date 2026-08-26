#ifndef RUSH_WKWEBVIEW_H
#define RUSH_WKWEBVIEW_H

#include <stdint.h>

typedef struct rush_wk_view rush_wk_view;

rush_wk_view *rush_wk_create(int debug, uintptr_t handle);
void rush_wk_run(rush_wk_view *view);
void rush_wk_terminate(rush_wk_view *view);
void rush_wk_destroy(rush_wk_view *view);
void rush_wk_dispatch(rush_wk_view *view, uintptr_t token);
void *rush_wk_window(rush_wk_view *view);
void rush_wk_set_title(rush_wk_view *view, const char *title);
void rush_wk_set_size(rush_wk_view *view, int width, int height, int hint);
void rush_wk_navigate(rush_wk_view *view, const char *url);
void rush_wk_set_html(rush_wk_view *view, const char *html);
void rush_wk_init(rush_wk_view *view, const char *javascript);
void rush_wk_eval(rush_wk_view *view, const char *javascript);
void rush_wk_evaluate(rush_wk_view *view, const char *javascript, uintptr_t token);
void rush_wk_snapshot(rush_wk_view *view, uintptr_t token);
int rush_wk_content_origin(rush_wk_view *view, double *x, double *y);
int rush_wk_trusted_click(double x, double y);
int rush_wk_trusted_type(const char *text);
int rush_wk_trusted_press(const char *key);

extern void goRushWKMessage(uintptr_t handle, char *message);
extern void goRushWKDispatch(uintptr_t token);
extern void goRushWKEvaluation(uintptr_t token, char *json, char *error);
extern void goRushWKSnapshot(uintptr_t token, void *bytes, int length, char *error);

#endif
