// Marks the toolbar icon with the build's badge (DEV for dev builds) so the
// dev and prod extensions are easy to tell apart when both are loaded.
type Action = Pick<typeof chrome.action, 'setBadgeText' | 'setBadgeBackgroundColor'>

export function showEnvironmentBadge(action: Action, badge: string): void {
  if (!badge) return
  void action.setBadgeBackgroundColor({ color: '#b45309' })
  void action.setBadgeText({ text: badge })
}
