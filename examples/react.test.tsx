import React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test } from "@rush/browser";

function SaveButton() {
  const [saved, setSaved] = React.useState(false);
  return (
    <button type="button" onClick={() => setSaved(true)}>
      {saved ? "Saved" : "Save"}
    </button>
  );
}

test("runs React Testing Library in the browser", () => {
  render(<SaveButton />);
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(screen.getByRole("button", { name: "Saved" })).toBeTruthy();
});
