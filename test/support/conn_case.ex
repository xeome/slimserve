defmodule SlimServeWeb.ConnCase do
  @moduledoc false
  use ExUnit.CaseTemplate

  using do
    quote do
      use SlimServeWeb, :verified_routes

      import Phoenix.ConnTest
      import Plug.Conn
      import SlimServeWeb.ConnCase

      @endpoint SlimServeWeb.Endpoint
    end
  end

  setup _tags do
    {:ok, conn: Phoenix.ConnTest.build_conn()}
  end
end
