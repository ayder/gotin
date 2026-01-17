# Task 3: Scrolling

## Goal
Handle large amounts of text and user navigation.

## Steps
1.  **Enable Viewport Scrolling**
    -   BubbleTea's `viewport` component handles this nativey if configured.
    -   Map keys (PageUp/PageDown/MouseWheel) to `m.viewport.Update(msg)`.

2.  **Auto-Scroll on New Content**
    -   When appending text, ensure `m.viewport.GotoBottom()` is called unless the user is actively scrolling up.

## Verification
-   Spam the input to fill the screen.
-   Verify you can scroll up to see old text.
-   Verify typing new text snaps back to bottom (if desired) or at least content updates.
