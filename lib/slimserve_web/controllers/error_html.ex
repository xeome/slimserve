defmodule SlimServeWeb.ErrorHTML do
  @moduledoc """
  See config/config.exs.
  """
  use SlimServeWeb, :html

  def render(template, _assigns) do
    Phoenix.Controller.status_message_from_template(template)
  end
end
