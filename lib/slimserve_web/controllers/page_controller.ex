defmodule SlimServeWeb.PageController do
  use SlimServeWeb, :controller

  def home(conn, _params) do
    render(conn, :home)
  end
end
