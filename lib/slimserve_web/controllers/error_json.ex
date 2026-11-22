defmodule SlimServeWeb.ErrorJSON do
  @moduledoc """
  See config/config.exs.
  """

  def render(template, _assigns) do
    %{errors: %{detail: Phoenix.Controller.status_message_from_template(template)}}
  end
end
