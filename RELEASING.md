# Releasing

Skiff's own server updates itself from `main`, so a release isn't how changes reach production. A release is for people installing the CLI or running their own server, and a version number should tell them something has been proven.

## When to release

- **Minor (0.x.0):** new features, in batches. Cut one after the batch has run on a real server for about a week without trouble. A feature landing isn't a reason to release.
- **Patch (0.x.y):** fixes only, never new features. A real bug or a security problem can ship the same day.
- **1.0:** when upgrades can be promised not to break people's setups.

## How to release

1. Check `main` has been running on production, and that CI passes, including the upgrade test from every earlier minor.
2. Write the notes for someone upgrading: what changed, why it matters to them, and anything they must do, such as restarting the router.
3. Tag `main` and push the tag:

   ```sh
   git tag v0.x.y && git push origin v0.x.y
   ```

   The release workflow builds the binaries, publishes the release and updates the Homebrew formula.

   Then move the GitHub Action's tag, so workflows using `nuelScript/skiff@v0` get this version:

   ```sh
   git tag -f v0 && git push -f origin v0
   ```
4. Attach the notes:

   ```sh
   gh release edit v0.x.y --notes-file notes.md
   ```

5. Check the install paths deliver it: the Homebrew formula's version, and a downloaded binary's `skiff --version`.
